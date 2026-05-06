package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/gmcorenet/gmcore/internal/apps"
	"github.com/gmcorenet/gmcore/internal/bundle"
	"github.com/gmcorenet/gmcore/internal/download"
	"github.com/gmcorenet/gmcore/internal/installer"
	"github.com/gmcorenet/gmcore/internal/manifest"
	"github.com/gmcorenet/gmcore/internal/update"
	"github.com/gmcorenet/gmcore/internal/version"
)

const cliVersion = "v0.5.0"
const repo = "gmcorenet/gmcore"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	if appRoot := detectAppRoot(); appRoot != "" {
		if err := runAppCommand(appRoot); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		return
	}

	scope := os.Args[1]

	switch scope {
	case "app":
		handleAppScope(os.Args[2:])

	case "bundle":
		handleBundleScope(os.Args[2:])

	case "self-update":
		targetVersion := ""
		if len(os.Args) >= 3 {
			targetVersion = os.Args[2]
		}
		if err := selfUpdate(targetVersion); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "update":
		handleUpdate(os.Args[2:])

	case "version", "--version", "-v":
		fmt.Printf("gmcore %s\n", cliVersion)

	case "install":
		if err := install(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "uninstall":
		purge := false
		confirmPurge := false
		for _, arg := range os.Args[2:] {
			if arg == "--purge" {
				purge = true
			} else if arg == "--confirm-purge" {
				confirmPurge = true
			}
		}
		if err := uninstallCLI(purge, confirmPurge); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", scope)
		printUsage()
		os.Exit(1)
	}
}

func handleAppScope(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: gmcore app <create|remove|list|status|start|stop|restart|reload|versions>")
		os.Exit(1)
	}

	subcmd := args[0]
	rest := args[1:]

	switch subcmd {
	case "create":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: gmcore app create <appname> [--version=<version>]")
			os.Exit(1)
		}
		appName := rest[0]
		frameworkVersion := "latest"
		for _, arg := range rest[1:] {
			if strings.HasPrefix(arg, "--version=") {
				frameworkVersion = strings.TrimPrefix(arg, "--version=")
			}
		}
		if err := createApp(appName, frameworkVersion); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "remove":
		purge := false
		appName := ""
		for _, arg := range rest {
			if arg == "--purge" {
				purge = true
			} else if !strings.HasPrefix(arg, "--") && appName == "" {
				appName = arg
			}
		}
		if appName == "" {
			fmt.Fprintln(os.Stderr, "Usage: gmcore app remove <appname> [--purge]")
			os.Exit(1)
		}
		if err := removeApp(appName, purge); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "list":
		if err := listApps(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "status":
		appName := ""
		if len(rest) >= 1 {
			appName = rest[0]
		}
		if err := statusApps(appName); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "start", "stop", "restart", "reload":
		if err := handleLifecycleCommand(subcmd, rest); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "versions":
		if err := listVersions(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown app command: %s\n", subcmd)
		fmt.Fprintln(os.Stderr, "Usage: gmcore app <create|remove|list|status|start|stop|restart|reload|versions>")
		os.Exit(1)
	}
}

func handleBundleScope(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: gmcore bundle <make|install|list>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  make <name>        Create a new bundle scaffold")
		fmt.Fprintln(os.Stderr, "  install <name>     Install a bundle from the registry")
		fmt.Fprintln(os.Stderr, "  list               List available bundles")
		os.Exit(1)
	}

	subcmd := args[0]
	rest := args[1:]

	switch subcmd {
	case "make":
		handleBundleMake(rest)
	case "install":
		handleBundleInstall(rest)
	case "list":
		if err := listBundles(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown bundle command: %s\n", subcmd)
		fmt.Fprintln(os.Stderr, "Usage: gmcore bundle <make|install|list>")
		os.Exit(1)
	}
}

func detectAppRoot() string {
	return apps.DetectFromCWD(getBasePath(), "")
}

func runAppCommand(appRoot string) error {
	appName := filepath.Base(appRoot)
	args := os.Args[1:]

	if len(args) == 0 {
		return listAppCommands(appRoot)
	}

	cmdName := args[0]

	if cmdName == "help" || cmdName == "--help" || cmdName == "-h" {
		return listAppCommands(appRoot)
	}

	binaryName := appName
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(appRoot, "bin", binaryName)

	if _, err := os.Stat(binaryPath); err == nil {
		cmd := exec.Command(binaryPath, args...)
		cmd.Dir = appRoot
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(),
			"GMCORE_APP_ROOT="+appRoot,
			"GMCORE_APP_NAME="+appName,
		)
		return cmd.Run()
	}

	scriptPath := filepath.Join(appRoot, "bin", "gmcore", "commands", cmdName)
	if runtime.GOOS != "windows" {
		if _, err := os.Stat(scriptPath); err == nil {
			cmd := exec.Command(scriptPath, args[1:]...)
			cmd.Dir = appRoot
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = append(os.Environ(), "GMCORE_APP_ROOT="+appRoot)
			return cmd.Run()
		}
		scriptPath += ".sh"
		if _, err := os.Stat(scriptPath); err == nil {
			cmd := exec.Command(scriptPath, args[1:]...)
			cmd.Dir = appRoot
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = append(os.Environ(), "GMCORE_APP_ROOT="+appRoot)
			return cmd.Run()
		}
	}

	return fmt.Errorf("unknown command %q or app binary not found", cmdName)
}

func listAppCommands(appRoot string) error {
	appName := filepath.Base(appRoot)
	fmt.Printf("GMCore App: %s\n", appName)
	fmt.Printf("App Root: %s\n", appRoot)
	fmt.Println("")

	binaryName := appName
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(appRoot, "bin", binaryName)

	if _, err := os.Stat(binaryPath); err == nil {
		fmt.Println("Commands: (via app binary)")
		fmt.Println("  Use app's built-in command system")
	} else {
		commandsDir := filepath.Join(appRoot, "bin", "gmcore", "commands")
		if entries, err := os.ReadDir(commandsDir); err == nil {
			fmt.Println("Commands:")
			for _, entry := range entries {
				name := entry.Name()
				if runtime.GOOS == "windows" && strings.HasSuffix(name, ".sh") {
					name = strings.TrimSuffix(name, ".sh")
				}
				fmt.Printf("  %s\n", name)
			}
		} else {
			fmt.Println("No commands found")
		}
	}
	return nil
}

func printUsage() {
	fmt.Println("gmcore - GMCore Application Framework CLI")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  gmcore app create <appname>              Create a new GMCore application")
	fmt.Println("  gmcore app remove <appname> [--purge]    Remove an application")
	fmt.Println("  gmcore app list                          List installed applications")
	fmt.Println("  gmcore app status [appname]              Show application status")
	fmt.Println("  gmcore app start [appname]               Start an application")
	fmt.Println("  gmcore app stop [appname]                Stop an application")
	fmt.Println("  gmcore app restart [appname]             Restart an application")
	fmt.Println("  gmcore app reload [appname]              Reload an application")
	fmt.Println("  gmcore app versions                      List available framework versions")
	fmt.Println("")
	fmt.Println("  gmcore bundle make <name>                Create a new bundle scaffold")
	fmt.Println("  gmcore bundle install <name>             Install a bundle from the registry")
	fmt.Println("  gmcore bundle list                       List available bundles")
	fmt.Println("")
	fmt.Println("  gmcore update [app] [flags]              Update framework, SDKs, or skeleton")
	fmt.Println("  gmcore self-update [version]             Update CLI to latest or specific version")
	fmt.Println("")
	fmt.Println("  gmcore version                           Show version information")
	fmt.Println("  gmcore install                           Install CLI to /usr/local/bin (requires root)")
	fmt.Println("  gmcore uninstall [--purge [--confirm-purge]]  Uninstall CLI")
	fmt.Println("")
	fmt.Println("Usage (local - run from within an app directory):")
	fmt.Println("  gmcore                                   List available commands")
	fmt.Println("  gmcore <command>                         Run app/bundle/SDK command")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  gmcore app create myapp")
	fmt.Println("  gmcore app remove myapp --purge")
	fmt.Println("  gmcore update myapp --target=all --rollback")
	fmt.Println("  sudo gmcore uninstall --purge --confirm-purge")
	fmt.Println("  cd <app-directory> && gmcore cache:clear")
}

func install() error {
	if err := requireRoot(); err != nil {
		return err
	}

	fmt.Println("Installing gmcore system-wide...")

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find current executable: %w", err)
	}

	var targetPath string
	switch runtime.GOOS {
	case "linux":
		targetPath = "/usr/local/bin/gmcore"
	case "darwin":
		targetPath = "/usr/local/bin/gmcore"
	case "windows":
		targetPath = "C:\\Program Files\\gmcore\\gmcore.exe"
		if err := os.MkdirAll("C:\\Program Files\\gmcore", 0755); err != nil {
			return fmt.Errorf("failed to create install directory: %w", err)
		}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if err := copyFile(exePath, targetPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(targetPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	fmt.Printf("Installed to %s\n", targetPath)
	return nil
}

func uninstallCLI(purge bool, confirmPurge bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine running executable: %w", err)
	}

	var targetPath string
	switch runtime.GOOS {
	case "linux", "darwin":
		targetPath = "/usr/local/bin/gmcore"
	case "windows":
		targetPath = "C:\\Program Files\\gmcore\\gmcore.exe"
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if purge {
		if os.Getuid() != 0 {
			return fmt.Errorf("--purge requires root privileges. Run with sudo")
		}

		if !confirmPurge {
			if os.Getenv("GMCORE_PURGE_CONFIRM") != "1" {
				fmt.Println("WARNING: This will remove ALL GMCore applications and their data!")
				fmt.Println("")
				fmt.Print("Type 'YES' to confirm: ")
				var confirmation string
				fmt.Scanln(&confirmation)
				if confirmation != "YES" {
					fmt.Println("Aborted.")
					return nil
				}
			}
		} else {
			if os.Getenv("GMCORE_PURGE_CONFIRM") != "1" {
				fmt.Fprintln(os.Stderr, "Error: --confirm-purge requires GMCORE_PURGE_CONFIRM=1 env var")
				return fmt.Errorf("insufficient confirmation")
			}
		}

		if exePath != targetPath {
			fmt.Fprintf(os.Stderr, "Warning: Running binary is not at %s\n", targetPath)
			fmt.Fprintln(os.Stderr, "Uninstall may not remove the correct binary.")
		}

		return purgeAllApps()
	}

	if exePath != targetPath {
		fmt.Fprintf(os.Stderr, "Error: uninstall only works when running the installed binary at:\n  %s\n", targetPath)
		return fmt.Errorf("must run installed binary to uninstall")
	}

	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove binary: %w", err)
	}

	fmt.Printf("Uninstalled gmcore from %s\n", targetPath)
	return nil
}

func purgeAllApps() error {
	basePath := getBasePath()
	logPath := filepath.Join(basePath, "purge.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		defer logFile.Close()
		fmt.Fprintf(logFile, "[%s] PURGE initiated by %d\n", time.Now().Format(time.RFC3339), os.Getuid())
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No applications to purge.")
			return nil
		}
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var appsToPurge []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "gmcore-") {
			continue
		}
		appsToPurge = append(appsToPurge, entry.Name())
	}

	if len(appsToPurge) == 0 {
		fmt.Println("No applications to purge.")
		return nil
	}

	fmt.Printf("Applications to purge (%d):\n", len(appsToPurge))
	for _, app := range appsToPurge {
		fmt.Printf("  - %s\n", app)
	}
	fmt.Println("")

	purgeCount := 0
	for _, appDir := range appsToPurge {
		appName := strings.TrimPrefix(appDir, "gmcore-")
		fmt.Printf("Purging application: %s...\n", appName)

		if logFile != nil {
			fmt.Fprintf(logFile, "[%s] Purging %s\n", time.Now().Format(time.RFC3339), appName)
		}

		if err := removeApp(appName, true); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to purge %s: %v\n", appName, err)
			if logFile != nil {
				fmt.Fprintf(logFile, "[%s] FAILED to purge %s: %v\n", time.Now().Format(time.RFC3339), appName, err)
			}
		}
		purgeCount++
	}

	fmt.Printf("Purged %d application(s).\n", purgeCount)

	if runtime.GOOS == "windows" {
		programFiles := "C:\\Program Files\\gmcore"
		os.RemoveAll(programFiles)
	} else {
		os.Remove("/usr/local/bin/gmcore")
	}
	fmt.Println("gmcore has been uninstalled.")

	if logFile != nil {
		fmt.Fprintf(logFile, "[%s] PURGE completed. Purged %d apps\n", time.Now().Format(time.RFC3339), purgeCount)
	}

	return nil
}

func copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

func createApp(appName, manifestVersion string) error {
	if appName == "" {
		return fmt.Errorf("appname cannot be empty")
	}

	if strings.Contains(appName, " ") || strings.Contains(appName, "/") {
		return fmt.Errorf("appname cannot contain spaces or slashes")
	}

	basePath := getBasePath()
	appPath := filepath.Join(basePath, appName)

	for _, candidate := range apps.CandidateDirs(appName) {
		candidatePath := filepath.Join(basePath, candidate)
		if _, err := os.Stat(candidatePath); err == nil {
			return fmt.Errorf("application %s already exists at %s", appName, candidatePath)
		}
	}

	if err := requireRoot(); err != nil {
		return fmt.Errorf("creating an app %s", err)
	}

	fmt.Printf("Creating user and group for %s...\n", appName)
	if err := createAppUser(appName); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	fmt.Printf("Creating application %s...\n", appName)

	fmt.Println("")
	fmt.Println("Fetching manifest...")
	m, err := manifest.Fetch("gmcorenet", "manifest", manifestVersion)
	if err != nil {
		return fmt.Errorf("failed to fetch manifest: %w", err)
	}

	fmt.Printf("Manifest: %s (version %s)\n", m.Name, m.Version)
	fmt.Println("")

	inst := installer.NewWithVars(appPath, true)
	instWithVars := installer.NewWithVars(appPath, true)

	fmt.Println("Installing framework...")
	framework := m.GetFramework()
	if err := inst.InstallComponent(installer.Component{
		Repo:    framework.Repo,
		Release: framework.Release,
		Path:    "vendor/framework",
	}); err != nil {
		return fmt.Errorf("failed to install framework: %w", err)
	}

	appVars := installer.BuildAppVars(appName, getGoVersion())
	fmt.Println("")
	fmt.Println("Installing skeleton with variable substitution...")
	skeleton := m.GetSkeleton()
	if err := instWithVars.InstallComponentWithVars(installer.Component{
		Repo:    skeleton.Repo,
		Release: skeleton.Release,
		Path:    ".",
	}, appVars); err != nil {
		return fmt.Errorf("failed to install skeleton: %w", err)
	}

	effectiveSDKs := m.EffectiveSDKs(appName)
	fmt.Println("")
	fmt.Printf("Installing %d SDKs...\n", len(effectiveSDKs))
	for _, sdk := range effectiveSDKs {
		fmt.Printf("  - %s (%s)\n", sdk.Name, sdk.Release)
		if err := inst.InstallComponent(installer.Component{
			Repo:    "gmcorenet/" + sdk.Name,
			Release: sdk.Release,
			Path:    "vendor/sdks/" + sdk.Name,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "    Warning: failed to install %s: %v\n", sdk.Name, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(appPath, "vendor"), 0755); err != nil {
		return fmt.Errorf("failed to create vendor directory: %w", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chown(appPath, getUID("gmcore-"+appName), getGID("gmcore")); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to change ownership: %v\n", err)
		}
	} else {
		userName := "gmcore-" + appName
		cmd := exec.Command("icacls", appPath, "/grant", userName+":(OI)(CI)F")
		cmd.Run()
	}

	if err := postCreateSetup(appPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: post-create setup failed: %v\n", err)
	}

	if err := ensureExposureAndTransportDefaults(appPath, appName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write exposure defaults: %v\n", err)
	}

	if runtime.GOOS == "windows" {
		fmt.Printf("Creating Windows service for %s...\n", appName)
		if err := createWindowsService(appName, appPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create service: %v\n", err)
		}
	}

	fmt.Println("")
	fmt.Printf("Application %s created successfully at %s\n", appName, appPath)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", appPath)
	fmt.Println("  go mod tidy")
	fmt.Println("  go build -o bin/myapp cmd/server/main.go")
	fmt.Printf("  gmcore status %s\n", appName)
	fmt.Printf("  gmcore start %s\n", appName)

	return nil
}

func createAppUser(appName string) error {
	userName := "gmcore-" + appName

	switch runtime.GOOS {
	case "linux":
		return createUserLinux(userName)
	case "darwin":
		return createUserMacOS(userName)
	case "windows":
		return createUserWindows(userName)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func createUserLinux(userName string) error {
	groupCmd := exec.Command("groupadd", "-f", "gmcore")
	if output, err := groupCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("groupadd failed: %s", string(output))
	}

	userCmd := exec.Command("useradd", "-M", "-s", "/usr/sbin/nologin", "-g", "gmcore", userName)
	if output, err := userCmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "already exists") {
			return nil
		}
		return fmt.Errorf("useradd failed: %s", string(output))
	}

	return nil
}

func createUserMacOS(userName string) error {
	if err := exec.Command("dscl", ".", "-create", "/Groups/gmcore").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dscl group creation: %v\n", err)
	}

	groupCmd := exec.Command("dscl", ".", "-append", "/Groups/gmcore", "GroupMembership", userName)
	if err := groupCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dscl group membership: %v\n", err)
	}

	userCmd := exec.Command("sysadminctl", "-addUser", userName, "-shell", "/usr/bin/false", "-n", userName)
	output, err := userCmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			return nil
		}
		return fmt.Errorf("sysadminctl failed: %s", string(output))
	}

	return nil
}

func createUserWindows(userName string) error {
	if err := exec.Command("net", "localgroup", "gmcore", "/add").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: net localgroup gmcore: %v\n", err)
	}

	cmd := exec.Command("net", "user", userName, "/add", "/active:no")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			return nil
		}
		return fmt.Errorf("net user failed: %s", string(output))
	}

	groupCmd := exec.Command("net", "localgroup", "gmcore", userName, "/add")
	if err := groupCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: net localgroup gmcore user: %v\n", err)
	}

	return nil
}

func removeApp(appName string, purge bool) error {
	if appName == "" {
		return fmt.Errorf("appname cannot be empty")
	}

	if err := requireRoot(); err != nil {
		return fmt.Errorf("removing an app %s", err)
	}

	entry, err := apps.ResolveByName(getBasePath(), appName)
	if err != nil {
		return err
	}

	appPath := entry.Path
	appName = entry.Name

	fmt.Printf("Stopping %s...\n", appName)
	stopApp(appName)

	fmt.Printf("Removing application files...\n")
	if err := os.RemoveAll(appPath); err != nil {
		return fmt.Errorf("failed to remove app directory: %w", err)
	}

	if purge {
		fmt.Printf("Purging all data (--purge specified)...\n")
		envPath := filepath.Join(appPath, ".env")
		if data, err := os.ReadFile(envPath); err == nil {
			content := string(data)
			if strings.Contains(content, "DATABASE_URL") || strings.Contains(content, "DB_") {
				fmt.Printf("  Found database config in .env - manual cleanup may be needed\n")
			}
		}
		varDirs := []string{"var", "data", "db", "storage"}
		for _, dir := range varDirs {
			purgePath := filepath.Join(appPath, dir)
			if _, err := os.Stat(purgePath); err == nil {
				fmt.Printf("  Removing %s/\n", dir)
				os.RemoveAll(purgePath)
			}
		}
	}

	if runtime.GOOS == "windows" {
		fmt.Printf("Removing Windows service %s...\n", "gmcore-"+appName)
		removeWindowsService(appName)
	}

	fmt.Printf("Removing user %s...\n", "gmcore-"+appName)
	if err := removeAppUser(appName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove user: %v\n", err)
	}

	fmt.Printf("Application %s removed successfully\n", appName)
	return nil
}

func stopApp(appName string) {
	switch runtime.GOOS {
	case "linux", "darwin":
		exec.Command("systemctl", "stop", "gmcore-"+appName+".service").Run()
		exec.Command("pkill", "-u", "gmcore-"+appName).Run()
	case "windows":
		exec.Command("net", "stop", "gmcore-"+appName).Run()
		exec.Command("taskkill", "/F", "/FI", "USERNAME eq gmcore-"+appName).Run()
	}
}

func createWindowsService(appName, appPath string) error {
	serviceName := "gmcore-" + appName
	binaryPath := filepath.Join(appPath, "bin", appName+".exe")

	cmd := exec.Command("sc", "create", serviceName, "binPath=", binaryPath, "obj=", "gmcore-"+appName, "start=", "demand")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "service already exists") {
			return nil
		}
		return fmt.Errorf("sc create failed: %s", string(output))
	}

	exec.Command("sc", "config", serviceName, "Description=", "GMCore application "+appName).Run()

	return nil
}

func removeWindowsService(appName string) error {
	serviceName := "gmcore-" + appName

	exec.Command("sc", "stop", serviceName).Run()
	cmd := exec.Command("sc", "delete", serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "service does not exist") {
		return fmt.Errorf("sc delete failed: %s", string(output))
	}
	return nil
}

func removeAppUser(appName string) error {
	userName := "gmcore-" + appName

	switch runtime.GOOS {
	case "linux":
		cmd := exec.Command("userdel", userName)
		if output, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(output), "does not exist") {
			return fmt.Errorf("userdel failed: %s", string(output))
		}
		return nil
	case "darwin":
		cmd := exec.Command("sysadminctl", "-deleteUser", userName)
		cmd.Run()
		return nil
	case "windows":
		exec.Command("net", "user", userName, "/delete").Run()
		return nil
	}
	return nil
}

func listApps() error {
	entries, err := apps.List(getBasePath())
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	fmt.Println("Installed applications:")
	fmt.Println("")

	if len(entries) == 0 {
		fmt.Println("  (none)")
		return nil
	}

	for _, entry := range entries {
		fmt.Printf("  %s\n", entry.Name)
	}

	return nil
}

func statusApps(appName string) error {
	if appName != "" {
		return statusSingleApp(appName)
	}

	entries, err := apps.List(getBasePath())
	if err != nil {
		return fmt.Errorf("failed to list applications: %w", err)
	}

	fmt.Println("Application status:")
	fmt.Println("")

	for _, entry := range entries {
		printAppStatus(entry)
	}

	return nil
}

func statusSingleApp(appName string) error {
	entry, err := apps.ResolveByName(getBasePath(), appName)
	if err != nil {
		return err
	}

	printAppStatus(entry)
	return nil
}

func printAppStatus(entry apps.Entry) {
	running, pid, err := pidStatus(entry.Path)
	status := "stopped"
	if err != nil {
		status = "unknown"
	}
	if !running && processRunningForAppUser(entry.Name) {
		running = true
	}
	if running {
		status = "running"
	}

	exposureMode := readExposureMode(entry.Path)
	if running && pid > 0 {
		fmt.Printf("  %s - %s (pid=%d, exposure=%s)\n", entry.Name, status, pid, exposureMode)
		return
	}
	if running {
		fmt.Printf("  %s - %s (exposure=%s)\n", entry.Name, status, exposureMode)
		return
	}
	fmt.Printf("  %s - %s (exposure=%s)\n", entry.Name, status, exposureMode)
}

func selfUpdate(targetVersion string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find executable: %w", err)
	}

	platform := getPlatform()
	arch := getArch()
	binaryName := fmt.Sprintf("gmcore-%s-%s", platform, arch)

	if targetVersion == "" {
		fmt.Println("Checking for latest version...")
		latest, err := getLatestRelease()
		if err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}
		targetVersion = latest
		fmt.Printf("Latest version: %s\n", targetVersion)
	}

	currentVersion := cliVersion
	if targetVersion == currentVersion {
		fmt.Printf("Already at version %s\n", currentVersion)
		return nil
	}

	fmt.Printf("Updating from %s to %s...\n", currentVersion, targetVersion)

	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, targetVersion, binaryName)

	tmpDir, err := os.MkdirTemp("", "gmcore-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpBinary := filepath.Join(tmpDir, "gmcore")

	if err := download.File(downloadURL, tmpBinary); err != nil {
		return fmt.Errorf("failed to download: %w\n\nVersion %s may not exist. Run 'gmcore-cli list-versions' to see available versions.", err, targetVersion)
	}

	if err := os.Chmod(tmpBinary, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if err := os.Rename(tmpBinary, exePath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("Updated to %s successfully\n", targetVersion)
	return nil
}

func getLatestRelease() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return release.TagName, nil
}

func getPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	default:
		return "linux"
	}
}

func getArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}

func postCreateSetup(appPath string) error {
	goModPath := filepath.Join(appPath, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		return nil
	}

	cmd := exec.Command("go", "mod", "edit", "-replace", "github.com/gmcorenet/framework=./vendor/framework")
	cmd.Dir = appPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod edit replace failed: %s", string(output))
	}

	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = appPath
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy failed: %s", string(output))
	}

	return nil
}

func extractTarGz(tarball, dest string) error {
	binPath, err := exec.LookPath("tar")
	if err != nil {
		return fmt.Errorf("tar not found: %w", err)
	}

	cmd := exec.Command(binPath, "-xzf", tarball, "-C", dest)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar extraction failed: %s", string(output))
	}

	return nil
}

func listVersions() error {
	versions, err := version.List()
	if err != nil {
		return err
	}

	fmt.Println("Available framework versions:")
	fmt.Println("")
	for _, v := range versions {
		fmt.Printf("  %s", v.Tag)
		if v.IsLatest {
			fmt.Print(" (latest)")
		}
		fmt.Println()
	}

	return nil
}

func getUID(username string) int {
	return getUserID(username)
}

func getGID(groupname string) int {
	return getGroupID(groupname)
}

func getUserID(username string) int {
	switch runtime.GOOS {
	case "linux":
		cmd := exec.Command("id", "-u", username)
		output, err := cmd.Output()
		if err != nil {
			return -1
		}
		var uid int
		fmt.Sscanf(string(output), "%d", &uid)
		return uid
	}
	return -1
}

func getBasePath() string {
	return apps.BasePath()
}

func getGoVersion() string {
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return "1.21"
	}
	version := string(output)
	re := regexp.MustCompile(`go(\d+\.\d+)`)
	matches := re.FindStringSubmatch(version)
	if len(matches) >= 2 {
		return matches[1]
	}
	return "1.21"
}

func toCamelCaseTitle(s string) string {
	if len(s) == 0 {
		return s
	}
	parts := strings.Split(s, "-")
	for i := range parts {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
		}
	}
	return strings.Join(parts, "")
}

func requireRoot() error {
	switch runtime.GOOS {
	case "windows":
		if !isWindowsAdmin() {
			return fmt.Errorf("requires administrator privileges. Run as administrator")
		}
		return nil
	default:
		if os.Getuid() != 0 {
			return fmt.Errorf("requires root privileges. Run with sudo")
		}
	}
	return nil
}

func isWindowsAdmin() bool {
	return exec.Command("net", "session").Run() == nil
}

func getGroupID(groupname string) int {
	switch runtime.GOOS {
	case "linux":
		cmd := exec.Command("getent", "group", groupname)
		output, err := cmd.Output()
		if err != nil {
			return -1
		}
		parts := strings.Split(string(output), ":")
		if len(parts) >= 3 {
			var gid int
			fmt.Sscanf(parts[2], "%d", &gid)
			return gid
		}
	}
	return -1
}

func installBundle(tier, name, version string) error {
	fmt.Printf("Installing bundle %s from %s tier...\n", name, tier)

	b, err := bundle.Fetch(tier, name, version)
	if err != nil {
		return fmt.Errorf("failed to fetch bundle: %w", err)
	}

	fmt.Printf("Bundle: %s\n", b.GetRepo())
	fmt.Printf("Version: %s (released: %s)\n", b.GetVersion(), b.GetReleased())
	fmt.Printf("Components: %d\n", len(b.GetComponents()))
	fmt.Println("")

	for _, comp := range b.GetComponents() {
		fmt.Printf("  - %s (%s)\n", comp.Name, comp.Version)
	}

	return nil
}

func listBundles() error {
	fmt.Println("Available bundles:")
	fmt.Println("")
	fmt.Println("Official tier:")
	fmt.Println("  (no bundles yet - submit via PR)")
	fmt.Println("")
	fmt.Println("Approved tier:")
	fmt.Println("  (no bundles yet)")
	fmt.Println("")
	fmt.Println("Wild tier:")
	fmt.Println("  (no bundles yet)")
	fmt.Println("")
	fmt.Println("Use: gmcore bundle <name> --tier=<tier> --version=<version>")
	fmt.Println("")

	return nil
}

func handleBundleMake(args []string) {
	bundleName := ""
	folder := ""

	for _, arg := range args {
		if strings.HasPrefix(arg, "--folder=") {
			folder = strings.TrimPrefix(arg, "--folder=")
		} else if !strings.HasPrefix(arg, "--") && bundleName == "" {
			bundleName = arg
		}
	}

	if bundleName == "" {
		fmt.Fprintln(os.Stderr, "Usage: gmcore bundle make <name> [--folder=<path>]")
		os.Exit(1)
	}

	if folder == "" {
		folder = "./" + bundleName
	}

	if err := createBundleScaffold(folder, bundleName); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Bundle '%s' scaffold created at %s\n", bundleName, folder)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Printf("  1. Edit %s/manifest.yaml with your components\n", folder)
	fmt.Printf("  2. Review the structure in %s/\n", folder)
	fmt.Printf("  3. Submit a PR to https://github.com/gmcorenet/bundles\n")
}

func createBundleScaffold(folder, name string) error {
	if err := os.MkdirAll(folder, 0755); err != nil {
		return fmt.Errorf("failed to create folder: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(folder, "cmd", name), 0755); err != nil {
		return fmt.Errorf("failed to create cmd folder: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(folder, "internal", name), 0755); err != nil {
		return fmt.Errorf("failed to create internal folder: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(folder, "services"), 0755); err != nil {
		return fmt.Errorf("failed to create services folder: %w", err)
	}

	manifest := fmt.Sprintf(`version: "1.0.0"
name: "%s"
released: "%s"
repo: YOUR_USERNAME/bundle-%s

components:
  %s:
    path: internal/%s
    version: "1.0.0"
    verify: true
`, name, time.Now().Format("2006-01-02"), name, name, name)

	manifestPath := filepath.Join(folder, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		return fmt.Errorf("failed to create manifest.yaml: %w", err)
	}

	mainGo := fmt.Sprintf(`package main

import "fmt"

func main() {
	fmt.Println("%s bundle v1.0.0")
}
`, name)
	mainPath := filepath.Join(folder, "cmd", name, "main.go")
	if err := os.WriteFile(mainPath, []byte(mainGo), 0644); err != nil {
		return fmt.Errorf("failed to create main.go: %w", err)
	}

	componentGo := fmt.Sprintf(`package %s

type %s struct{}

func New() *%s {
	return &%s{}
}

func (b *%s) Run() error {
	return nil
}
`, name, toCamelCaseTitle(name), name, name, name)
	componentPath := filepath.Join(folder, "internal", name, name+".go")
	if err := os.WriteFile(componentPath, []byte(componentGo), 0644); err != nil {
		return fmt.Errorf("failed to create component.go: %w", err)
	}

	readme := fmt.Sprintf(`# Bundle %s

GMCore bundle for %s functionality.

## Structure

- manifest.yaml
- cmd/
  - %s/
    - main.go
- internal/
  - %s/
    - %s.go
- services/

## Publishing

1. Fork https://github.com/gmcorenet/bundles
2. Copy this folder to the appropriate tier
3. Submit a pull request
`, name, name, name, name, name)

	readmePath := filepath.Join(folder, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to create README.md: %w", err)
	}

	goMod := fmt.Sprintf(`module github.com/YOUR_USERNAME/bundle-%s

go 1.21
`, name)
	goModPath := filepath.Join(folder, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goMod), 0644); err != nil {
		return fmt.Errorf("failed to create go.mod: %w", err)
	}

	return nil
}

func handleBundleInstall(args []string) {
	tier := "official"
	bundleName := ""
	version := "latest"

	for _, arg := range args {
		if strings.HasPrefix(arg, "--tier=") {
			tier = strings.TrimPrefix(arg, "--tier=")
		} else if strings.HasPrefix(arg, "--version=") {
			version = strings.TrimPrefix(arg, "--version=")
		} else if !strings.HasPrefix(arg, "--") && bundleName == "" {
			bundleName = arg
		}
	}

	if bundleName == "" {
		fmt.Fprintln(os.Stderr, "Usage: gmcore bundle install <name> [--tier=official] [--version=latest]")
		os.Exit(1)
	}

	if err := installBundle(tier, bundleName, version); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func handleUpdate(args []string) {
	target := update.TargetAll
	version := "latest"
	appName := ""
	rollback := false
	verbose := false
	force := false
	var sdks []string

	for _, arg := range args {
		if strings.HasPrefix(arg, "--target=") {
			targetStr := strings.TrimPrefix(arg, "--target=")
			switch targetStr {
			case "framework":
				target = update.TargetFramework
			case "sdks":
				target = update.TargetSDKs
			case "skeleton":
				target = update.TargetSkeleton
			case "app":
				target = update.TargetApp
			case "all":
				target = update.TargetAll
			default:
				fmt.Fprintf(os.Stderr, "Unknown target: %s\n", targetStr)
				fmt.Fprintln(os.Stderr, "Valid targets: framework, sdks, skeleton, app, all")
				os.Exit(1)
			}
		} else if strings.HasPrefix(arg, "--version=") {
			version = strings.TrimPrefix(arg, "--version=")
		} else if strings.HasPrefix(arg, "--app=") {
			appName = strings.TrimPrefix(arg, "--app=")
		} else if arg == "--rollback" {
			rollback = true
		} else if arg == "--verbose" || arg == "-v" {
			verbose = true
		} else if arg == "--force" || arg == "-f" {
			force = true
		} else if strings.HasPrefix(arg, "--sdk=") {
			sdkName := strings.TrimPrefix(arg, "--sdk=")
			sdks = append(sdks, sdkName)
		} else if arg == "--help" || arg == "-h" {
			printUpdateUsage()
			os.Exit(0)
		} else if !strings.HasPrefix(arg, "--") && appName == "" {
			appName = arg
		}
	}

	opts := &update.UpdateOptions{
		Target:   target,
		Version:  version,
		SDKs:     sdks,
		AppName:  appName,
		Rollback: rollback,
		Verbose:  verbose,
		Force:    force,
	}

	manager := update.NewUpdateManager(opts)
	if err := manager.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		os.Exit(1)
	}
}

func printUpdateUsage() {
	fmt.Println("gmcore update - Update framework, SDKs, and skeleton")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  gmcore update [app] [flags]")
	fmt.Println("  (if app is omitted, uses current directory if inside an app)")




	fmt.Println("")
	fmt.Println("Flags:")
	fmt.Println("  --target=<target>    Target to update: framework, sdks, skeleton, app, all (default: all)")
	fmt.Println("  --version=<version> Version to install (default: latest)")
	fmt.Println("  --app=<name>       Application name (optional if run from app directory)")
	fmt.Println("  --sdk=<name>        Specific SDK to update (can be used multiple times)")
	fmt.Println("  --rollback         Rollback on failure (saves backup to <app>/var/backups/)")
	fmt.Println("  --force, -f        Force merge of protected files (skip confirmation)")
	fmt.Println("  --verbose, -v       Verbose output")
	fmt.Println("  --help, -h          Show this help")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  gmcore update                          # update current app (if in app dir)")
	fmt.Println("  gmcore update myapp                    # update specific app")
	fmt.Println("  gmcore update --target=framework --version=v1.0.0")
	fmt.Println("  gmcore update myapp --target=sdks --sdk=gmcore-orm --sdk=gmcore-log")
	fmt.Println("  gmcore update myapp --target=skeleton --force  # merge protected files")
	fmt.Println("  gmcore update myapp --target=all --rollback --verbose")
	fmt.Println("")
	fmt.Println("Rollback backups are stored at:")
	fmt.Println("  <app-directory>/var/backups/<target>_<version>_<timestamp>.tar.gz")
}
