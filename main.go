package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gmcorenet/cli/internal/download"
	"github.com/gmcorenet/cli/internal/version"
)

const cliVersion = "0.1.0"
const repo = "gmcorenet/cli"

var availableCommands = []string{"create", "remove", "list", "status", "version", "self-update"}

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

	cmd := os.Args[1]

	switch cmd {
	case "create":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: gmcore-cli create <appname> [--version=<version>]")
			os.Exit(1)
		}
		appName := os.Args[2]
		frameworkVersion := "latest"
		for _, arg := range os.Args[3:] {
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
		for _, arg := range os.Args[2:] {
			if arg == "--purge" {
				purge = true
			} else if !strings.HasPrefix(arg, "--") && appName == "" {
				appName = arg
			}
		}
		if appName == "" {
			fmt.Fprintln(os.Stderr, "Usage: gmcore-cli remove <appname> [--purge]")
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
		if len(os.Args) >= 3 {
			appName = os.Args[2]
		}
		if err := statusApps(appName); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "list-versions":
		if err := listVersions(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

	case "self-update":
		targetVersion := ""
		if len(os.Args) >= 3 {
			targetVersion = os.Args[2]
		}
		if err := selfUpdate(targetVersion); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

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
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func detectAppRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	basePath := getBasePath()

	cwdNormalized := filepath.ToSlash(cwd)
	basePathNormalized := filepath.ToSlash(basePath)

	if !strings.HasPrefix(cwdNormalized, basePathNormalized) {
		return ""
	}

	relative := strings.TrimPrefix(cwdNormalized, basePathNormalized)
	relative = strings.TrimPrefix(relative, "/")

	if relative == "" || relative == "." {
		return ""
	}

	if strings.Contains(relative, "..") {
		return ""
	}

	parts := strings.SplitN(relative, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		return ""
	}

	appName := parts[0]
	appRoot := filepath.Join(basePath, appName)
	if _, err := os.Stat(appRoot); err != nil {
		return ""
	}

	return appRoot
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
	fmt.Println("gmcore-cli - GMCore Application Framework CLI")
	fmt.Println("")
	fmt.Println("Usage (global):")
	fmt.Println("  gmcore-cli create <appname>        Create a new GMCore application")
	fmt.Println("  gmcore-cli remove <appname [--purge]>  Remove an application")
	fmt.Println("  gmcore-cli list                   List installed applications")
	fmt.Println("  gmcore-cli status [appname]       Show application status")
	fmt.Println("  gmcore-cli list-versions          List available framework versions")
	fmt.Println("  gmcore-cli self-update [version] Update CLI to latest or specific version")
	fmt.Println("  gmcore-cli version               Show version information")
	fmt.Println("  gmcore-cli install               Install CLI (requires root/sudo)")
	fmt.Println("  gmcore-cli uninstall [--purge [--confirm-purge]]  Uninstall CLI")
	fmt.Println("")
	fmt.Println("Usage (local - run from within an app directory):")
	fmt.Println("  gmcore-cli                        List available commands")
	fmt.Println("  gmcore-cli <command>              Run app/bundle/SDK command")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  gmcore-cli create myapp")
	fmt.Println("  gmcore-cli remove myapp")
	fmt.Println("  gmcore-cli remove myapp --purge")
	fmt.Println("  gmcore-cli status")
	fmt.Println("  sudo gmcore-cli uninstall --purge --confirm-purge")
	fmt.Println("")
	fmt.Println("Local example:")
	fmt.Println("  cd /opt/gmcore/myapp")
	fmt.Println("  gmcore-cli cache:clear")
}

func install() error {
	if err := requireRoot(); err != nil {
		return err
	}

	fmt.Println("Installing gmcore-cli system-wide...")

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find current executable: %w", err)
	}

	var targetPath string
	switch runtime.GOOS {
	case "linux":
		targetPath = "/usr/local/bin/gmcore-cli"
	case "darwin":
		targetPath = "/usr/local/bin/gmcore-cli"
	case "windows":
		targetPath = "C:\\Program Files\\gmcore-cli\\gmcore-cli.exe"
		if err := os.MkdirAll("C:\\Program Files\\gmcore-cli", 0755); err != nil {
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
		targetPath = "/usr/local/bin/gmcore-cli"
	case "windows":
		targetPath = "C:\\Program Files\\gmcore-cli\\gmcore-cli.exe"
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
				fmt.Fprintln(os.Stderr, "Error: --confirm-purge requires GMCORE_PURGE_CONFIRM=1 environment variable")
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

	fmt.Printf("Uninstalled gmcore-cli from %s\n", targetPath)
	return nil
}

func purgeAllApps() error {
	basePath := getBasePath()
	logPath := "/var/log/gmcore-purge.log"
	if runtime.GOOS == "windows" {
		logPath = "C:\\ProgramData\\gmcore\\purge.log"
	}

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
		programFiles := "C:\\Program Files\\gmcore-cli"
		os.RemoveAll(programFiles)
	} else {
		os.Remove("/usr/local/bin/gmcore-cli")
	}
	fmt.Println("gmcore-cli has been uninstalled.")

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

func createApp(appName, frameworkVersion string) error {
	if appName == "" {
		return fmt.Errorf("appname cannot be empty")
	}

	if strings.Contains(appName, " ") || strings.Contains(appName, "/") {
		return fmt.Errorf("appname cannot contain spaces or slashes")
	}

	basePath := getBasePath()
	appPath := filepath.Join(basePath, appName)

	if _, err := os.Stat(appPath); err == nil {
		return fmt.Errorf("application %s already exists at %s", appName, appPath)
	}

	if err := requireRoot(); err != nil {
		return fmt.Errorf("creating an app %s", err)
	}

	fmt.Printf("Creating user and group for %s...\n", appName)
	if err := createAppUser(appName); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	fmt.Printf("Creating application %s with framework %s...\n", appName, frameworkVersion)

	versionInfo, err := version.Resolve(frameworkVersion)
	if err != nil {
		return fmt.Errorf("failed to resolve version: %w", err)
	}

	downloadURL := fmt.Sprintf("https://github.com/gmcorenet/sdk/archive/refs/tags/%s.tar.gz", versionInfo.Tag)

	tmpDir, err := os.MkdirTemp("", "gmcore-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "framework.tar.gz")

	if err := download.File(downloadURL, tarballPath); err != nil {
		return fmt.Errorf("failed to download framework: %w", err)
	}

	if err := os.MkdirAll(appPath, 0755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	if err := extractTarGz(tarballPath, appPath); err != nil {
		os.RemoveAll(appPath)
		return fmt.Errorf("failed to extract framework: %w", err)
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

	if runtime.GOOS == "windows" {
		fmt.Printf("Creating Windows service for %s...\n", appName)
		if err := createWindowsService(appName, appPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create service: %v\n", err)
		}
	}

	fmt.Printf("Application %s created successfully at %s\n", appName, appPath)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", appPath)
	fmt.Println("  go mod tidy")
	fmt.Println("  gmcore-cli status", appName)

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

	appPath := filepath.Join(getBasePath(), appName)

	if _, err := os.Stat(appPath); os.IsNotExist(err) {
		return fmt.Errorf("application %s does not exist at %s", appName, appPath)
	}

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
	basePath := getBasePath()

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No applications installed")
			return nil
		}
		return fmt.Errorf("failed to read directory: %w", err)
	}

	fmt.Println("Installed applications:")
	fmt.Println("")

	hasApps := false
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "gmcore-") {
			appName := strings.TrimPrefix(entry.Name(), "gmcore-")
			hasApps = true
			fmt.Printf("  %s\n", appName)
		}
	}

	if !hasApps {
		fmt.Println("  (none)")
	}

	return nil
}

func statusApps(appName string) error {
	basePath := "/opt/gmcore"

	if appName != "" {
		return statusSingleApp(appName)
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read directory: %w", err)
	}

	fmt.Println("Application status:")
	fmt.Println("")

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "gmcore-") {
			appName := strings.TrimPrefix(entry.Name(), "gmcore-")
			printAppStatus(appName)
		}
	}

	return nil
}

func statusSingleApp(appName string) error {
	appPath := filepath.Join(getBasePath(), appName)

	if _, err := os.Stat(appPath); os.IsNotExist(err) {
		return fmt.Errorf("application %s does not exist", appName)
	}

	printAppStatus(appName)
	return nil
}

func printAppStatus(appName string) {
	userName := "gmcore-" + appName

	isRunning := false
	switch runtime.GOOS {
	case "linux", "darwin":
		cmd := exec.Command("pgrep", "-u", userName)
		err := cmd.Run()
		isRunning = err == nil
	case "windows":
		cmd := exec.Command("tasklist", "/FI", "USERNAME eq "+userName)
		output, _ := cmd.Output()
		isRunning = !strings.Contains(string(output), "INFO: No tasks are running")
	}

	status := "stopped"
	if isRunning {
		status = "running"
	}

	fmt.Printf("  %s - %s\n", appName, status)
}

func selfUpdate(targetVersion string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find executable: %w", err)
	}

	platform := getPlatform()
	arch := getArch()
	binaryName := fmt.Sprintf("gmcore-cli-%s-%s", platform, arch)

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

	tmpBinary := filepath.Join(tmpDir, "gmcore-cli")

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

	data, _ := io.ReadAll(resp.Body)
	s := string(data)

	if idx := strings.Index(s, "\"tag_name\""); idx != -1 {
		start := strings.Index(s[idx:], "\"") + idx + 1
		end := start + strings.Index(s[start:], "\"")
		return strings.TrimSpace(s[start:end]), nil
	}

	return "", fmt.Errorf("tag_name not found in response")
}

func getPlatform() string {
	switch strings.ToLower(os.Getenv("GOOS")) {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	default:
		return "linux"
	}
}

func getArch() string {
	switch strings.ToLower(os.Getenv("GOARCH")) {
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

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appPath
	output, err := cmd.CombinedOutput()
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
	switch runtime.GOOS {
	case "windows":
		return "C:\\ProgramData\\gmcore"
	default:
		return "/opt/gmcore"
	}
}

func requireRoot() error {
	switch runtime.GOOS {
	case "windows":
		return nil
	default:
		if os.Getuid() != 0 {
			return fmt.Errorf("requires root privileges. Run with sudo")
		}
	}
	return nil
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

