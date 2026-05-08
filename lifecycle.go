package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gmcorenet/gmcore/internal/apps"
	gmcore_transport "github.com/gmcorenet/sdk-gmcore-transport"
	"gopkg.in/yaml.v3"
)

type transportMessage struct {
	Type      string          `json:"type"`
	Body      json.RawMessage `json:"body,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

type transportLifecycleResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

type tcpListenerConfig struct {
	Host  string `yaml:"host"`
	Ports []int  `yaml:"ports"`
}

type transportConfigForCLI struct {
	Server struct {
		Mode string `yaml:"mode"`
		UDS  struct {
			Path string `yaml:"path"`
		} `yaml:"uds"`
		TCP struct {
			Host      string             `yaml:"host"`
			Ports     []int              `yaml:"ports"`
			Listeners []tcpListenerConfig `yaml:"listeners"`
		} `yaml:"tcp"`
	} `yaml:"server"`
}

func handleLifecycleCommand(action string, args []string) error {
	appName := ""
	hotReload := false
	forceBuild := false
	verbose := false
	for _, a := range args {
		if a == "--hot-reload" || a == "-w" {
			hotReload = true
		} else if a == "--build" || a == "-b" {
			forceBuild = true
		} else if a == "--verbose" || a == "-v" {
			verbose = true
		} else if !strings.HasPrefix(a, "--") && appName == "" {
			appName = strings.TrimSpace(a)
		}
	}

	entry, err := resolveLifecycleTarget(appName)
	if err != nil {
		return err
	}

	switch action {
	case "start":
		return startManagedApp(entry, hotReload, forceBuild, verbose)
	case "stop":
		return stopManagedApp(entry)
	case "restart":
		if err := stopManagedApp(entry); err != nil {
			return err
		}
		return startManagedApp(entry, hotReload, forceBuild, verbose)
	case "reload":
		return reloadManagedApp(entry)
	default:
		return fmt.Errorf("unsupported lifecycle action: %s", action)
	}
}

func resolveLifecycleTarget(appName string) (apps.Entry, error) {
	basePath := getBasePath()
	if appName != "" {
		return apps.ResolveByName(basePath, appName)
	}

	appRoot := detectAppRoot()
	if appRoot == "" {
		return apps.Entry{}, errors.New("application name is required when not running inside an app directory")
	}

	return apps.Entry{
		Name: apps.NormalizeName(filepath.Base(appRoot)),
		Dir:  filepath.Base(appRoot),
		Path: appRoot,
	}, nil
}

func startManagedApp(entry apps.Entry, hotReload, forceBuild, verbose bool) error {
	if running, _, err := pidStatus(entry.Path); err == nil && running {
		fmt.Printf("Application %s is already running\n", entry.Name)
		return nil
	}

	if started, err := startViaService(entry.Name); err != nil {
		return err
	} else if started {
		fmt.Printf("Application %s started via service manager\n", entry.Name)
		return nil
	}

	if hotReload {
		return startWithHotReload(entry)
	}

	if !forceBuild && binaryExists(entry) {
		fmt.Printf("Binary exists, skipping build. Use --build to force recompile.\n")
	} else {
		if err := compileApp(entry); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	binaryPath, err := resolveAppBinary(entry.Path, entry.Name)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(entry.Path, "var", "run"), 0755); err != nil {
		return fmt.Errorf("failed to create run directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(entry.Path, "var", "log"), 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	cmd := exec.Command(binaryPath)
	cmd.Dir = entry.Path
	cmd.Env = append(os.Environ(),
		"GMCORE_APP_ROOT="+entry.Path,
		"GMCORE_APP_NAME="+entry.Name,
		"GMCORE_MANAGED_LAUNCH=1",
	)

	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		logPath := filepath.Join(entry.Path, "var", "log", "app.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open app log: %w", err)
		}
		defer logFile.Close()
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start app binary: %w", err)
	}

	pid := cmd.Process.Pid

	if !verbose {
		if err := cmd.Process.Release(); err != nil {
			return fmt.Errorf("failed to detach app process: %w", err)
		}
	}

	if err := writePIDFile(entry.Path, pid); err != nil {
		return err
	}

	fmt.Printf("Application %s started (pid=%d)\n", entry.Name, pid)
	return nil
}

func stopManagedApp(entry apps.Entry) error {
	if err := requireRoot(); err != nil {
		return fmt.Errorf("stopping an app %s", err)
	}

	if stopped, err := stopViaService(entry.Name); err != nil {
		return err
	} else if stopped {
		fmt.Printf("Application %s stopped via service manager\n", entry.Name)
		_ = removePIDFile(entry.Path)
		return nil
	}

	_, _ = sendLifecycleTransportCommand(entry, "stop")

	running, pid, err := pidStatus(entry.Path)
	if err != nil {
		return err
	}
	if !running {
		fmt.Printf("Application %s is already stopped\n", entry.Name)
		return nil
	}

	if err := terminatePID(pid); err != nil {
		return err
	}

	_ = removePIDFile(entry.Path)
	fmt.Printf("Application %s stopped\n", entry.Name)
	return nil
}

func reloadManagedApp(entry apps.Entry) error {
	if ok, err := sendLifecycleTransportCommand(entry, "reload"); err == nil && ok {
		fmt.Printf("Application %s reloaded via transport\n", entry.Name)
		return nil
	}

	running, pid, err := pidStatus(entry.Path)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("application %s is not running", entry.Name)
	}

	if runtime.GOOS == "windows" {
		return fmt.Errorf("reload fallback by signal is not supported on windows for %s", entry.Name)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("failed to send reload signal: %w", err)
	}

	fmt.Printf("Application %s reloaded (signal HUP)\n", entry.Name)
	return nil
}

func resolveAppBinary(appPath, appName string) (string, error) {
	binName := appName
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	candidates := []string{
		filepath.Join(appPath, "bin", binName),
		filepath.Join(appPath, "bin", "app"),
		filepath.Join(appPath, "bin", "server"),
	}

	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			filepath.Join(appPath, "bin", "app.exe"),
			filepath.Join(appPath, "bin", "server.exe"),
		)
	}

	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("app binary not found for %s under %s/bin", appName, appPath)
}

func binaryExists(entry apps.Entry) bool {
	_, err := resolveAppBinary(entry.Path, entry.Name)
	return err == nil
}

func pidFilePath(appPath string) string {
	return filepath.Join(appPath, "var", "run", "app.pid")
}

func writePIDFile(appPath string, pid int) error {
	path := pidFilePath(appPath)
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to write pid file: %w", err)
	}
	return nil
}

func removePIDFile(appPath string) error {
	path := pidFilePath(appPath)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readPIDFile(appPath string) (int, error) {
	path := pidFilePath(appPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	value := strings.TrimSpace(string(data))
	if value == "" {
		return 0, errors.New("empty pid file")
	}

	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid value: %s", value)
	}

	return pid, nil
}

func pidStatus(appPath string) (bool, int, error) {
	pid, err := readPIDFile(appPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}

	if processExists(pid) {
		return true, pid, nil
	}

	_ = removePIDFile(appPath)
	return false, 0, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false
		}
		return strings.Contains(string(output), strconv.Itoa(pid))
	}

	err := syscall.Kill(pid, 0)
	return err == nil
}

func processRunningForAppUser(appName string) bool {
	userName := "gmcore-" + appName

	switch runtime.GOOS {
	case "linux", "darwin":
		cmd := exec.Command("pgrep", "-u", userName)
		return cmd.Run() == nil
	case "windows":
		cmd := exec.Command("tasklist", "/FI", "USERNAME eq "+userName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false
		}
		return !strings.Contains(string(output), "INFO: No tasks are running")
	default:
		return false
	}
}

func terminatePID(pid int) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("taskkill failed: %s", string(output))
		}
		return nil
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("failed to send SIGKILL: %w", err)
	}

	return nil
}

func startViaService(appName string) (bool, error) {
	serviceName := "gmcore-" + appName

	switch runtime.GOOS {
	case "linux", "darwin":
		if _, err := exec.LookPath("systemctl"); err != nil {
			return false, nil
		}
		status := exec.Command("systemctl", "status", serviceName+".service")
		if err := status.Run(); err != nil {
			return false, nil
		}
		start := exec.Command("systemctl", "start", serviceName+".service")
		if output, err := start.CombinedOutput(); err != nil {
			return true, fmt.Errorf("systemctl start failed: %s", string(output))
		}
		return true, nil
	case "windows":
		query := exec.Command("sc", "query", serviceName)
		if err := query.Run(); err != nil {
			return false, nil
		}
		start := exec.Command("sc", "start", serviceName)
		if output, err := start.CombinedOutput(); err != nil {
			return true, fmt.Errorf("sc start failed: %s", string(output))
		}
		return true, nil
	default:
		return false, nil
	}
}

func stopViaService(appName string) (bool, error) {
	serviceName := "gmcore-" + appName

	switch runtime.GOOS {
	case "linux", "darwin":
		if _, err := exec.LookPath("systemctl"); err != nil {
			return false, nil
		}
		status := exec.Command("systemctl", "status", serviceName+".service")
		if err := status.Run(); err != nil {
			return false, nil
		}
		stop := exec.Command("systemctl", "stop", serviceName+".service")
		if output, err := stop.CombinedOutput(); err != nil {
			return true, fmt.Errorf("systemctl stop failed: %s", string(output))
		}
		return true, nil
	case "windows":
		query := exec.Command("sc", "query", serviceName)
		if err := query.Run(); err != nil {
			return false, nil
		}
		stop := exec.Command("sc", "stop", serviceName)
		if output, err := stop.CombinedOutput(); err != nil {
			return true, fmt.Errorf("sc stop failed: %s", string(output))
		}
		return true, nil
	default:
		return false, nil
	}
}

func sendLifecycleTransportCommand(entry apps.Entry, action string) (bool, error) {
	network, address, err := resolveTransportEndpoint(entry)
	if err != nil {
		return false, err
	}

	conn, err := net.DialTimeout(network, address, 2*time.Second)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	msg := transportMessage{Type: action, Timestamp: time.Now().Unix()}
	data, err := json.Marshal(msg)
	if err != nil {
		return false, err
	}

	if _, err := conn.Write(data); err != nil {
		return false, err
	}

	respData := make([]byte, 64*1024)
	n, err := conn.Read(respData)
	if err != nil {
		return false, err
	}

	if n == 0 {
		return false, errors.New("empty lifecycle response")
	}

	var resp transportLifecycleResponse
	if err := json.Unmarshal(respData[:n], &resp); err == nil {
		if !resp.Success {
			if resp.Error != "" {
				return false, errors.New(resp.Error)
			}
			return false, fmt.Errorf("lifecycle command failed with status=%s", resp.Status)
		}
		return true, nil
	}

	return true, nil
}

func resolveTransportEndpoint(entry apps.Entry) (string, string, error) {
	cfg := loadTransportConfigForCLI(entry.Path)

	mode := strings.ToLower(strings.TrimSpace(cfg.Server.Mode))

	var host string
	var ports []int

	if len(cfg.Server.TCP.Listeners) > 0 {
		first := cfg.Server.TCP.Listeners[0]
		host = strings.TrimSpace(first.Host)
		ports = first.Ports
	} else {
		host = strings.TrimSpace(cfg.Server.TCP.Host)
		ports = cfg.Server.TCP.Ports
	}
	if host == "" {
		host = "127.0.0.1"
	}

	if (mode == "tcp" || mode == "both") && len(ports) > 0 {
		port := ports[0]
		if port <= 0 {
			return "", "", fmt.Errorf("invalid transport port for %s", entry.Name)
		}
		return "tcp", net.JoinHostPort(host, strconv.Itoa(port)), nil
	}

	if runtime.GOOS == "windows" {
		if len(ports) == 0 {
			return "", "", fmt.Errorf("windows lifecycle requires TCP transport configuration for %s", entry.Name)
		}
		return "tcp", net.JoinHostPort(host, strconv.Itoa(ports[0])), nil
	}

	socketPath := strings.TrimSpace(cfg.Server.UDS.Path)
	if socketPath == "" {
		socketPath = filepath.Join(entry.Path, "var", "socket", entry.Name+".sock")
	} else if !filepath.IsAbs(socketPath) {
		socketPath = filepath.Join(entry.Path, socketPath)
	}

	return "unix", socketPath, nil
}

func loadTransportConfigForCLI(appPath string) transportConfigForCLI {
	paths := []string{
		filepath.Join(appPath, "config", "transport.yaml"),
		filepath.Join(appPath, "config", "transport.yml"),
	}

	var cfg transportConfigForCLI
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err := yaml.Unmarshal(data, &cfg); err == nil {
			return cfg
		}
	}

	return transportConfigForCLI{}
}

func compileApp(entry apps.Entry) error {
	fmt.Printf("Building %s...\n", entry.Name)

	cmd := exec.Command("go", "build", "-o", filepath.Join("bin", entry.Name), "./cmd/")
	cmd.Dir = entry.Path
	cmd.Env = append(os.Environ(), "GOOS="+runtime.GOOS)

	if runtime.GOOS == "windows" {
		cmd.Args[3] = filepath.Join("bin", entry.Name+".exe")
		cmd.Env = append(cmd.Env, "GOARCH="+getArch())
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %s", string(output))
	}

	if len(output) > 0 {
		fmt.Print(string(output))
	}

	fmt.Printf("Built %s/bin/%s\n", entry.Path, entry.Name)
	return nil
}

func startWithHotReload(entry apps.Entry) error {
	if _, err := exec.LookPath("air"); err != nil {
		fmt.Println("Installing air (hot-reload tool)...")
		install := exec.Command("go", "install", "github.com/air-verse/air@latest")
		install.Stdout = os.Stdout
		install.Stderr = os.Stderr
		if err := install.Run(); err != nil {
			return fmt.Errorf("failed to install air: %w", err)
		}
	}

	airConfig := filepath.Join(entry.Path, ".air.toml")
	if _, err := os.Stat(airConfig); os.IsNotExist(err) {
		toml := fmt.Sprintf(`root = "."
tmp_dir = "var/tmp"

[build]
  cmd = "go build -o var/tmp/app ./cmd/"
  bin = "var/tmp/app"
  include_ext = ["go", "yaml", "yml"]
  exclude_dir = ["var", "vendor", ".git"]
  delay = 500

[log]
  time = true

[misc]
  clean_on_exit = true
`)
		os.WriteFile(airConfig, []byte(toml), 0644)
		fmt.Printf("Created %s\n", airConfig)
	}

	fmt.Printf("Starting %s with hot-reload...\n", entry.Name)

	cmd := exec.Command("air", "-c", airConfig)
	cmd.Dir = entry.Path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"GMCORE_APP_ROOT="+entry.Path,
		"GMCORE_APP_NAME="+entry.Name,
	)
	return cmd.Run()
}

func generatePairCode(appName string) error {
	basePath := getBasePath()
	entry, err := apps.ResolveByName(basePath, appName)
	if err != nil {
		return err
	}

	network, addr, err := resolveTransportEndpoint(entry)
	if err != nil {
		return fmt.Errorf("failed to resolve transport endpoint for %s: %w", appName, err)
	}

	conn, err := net.DialTimeout(network, addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", appName, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	msg := transportMessage{Type: "pair_generate", Timestamp: time.Now().Unix()}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("failed to send pair_generate: %w", err)
	}

	respData := make([]byte, 64*1024)
	n, err := conn.Read(respData)
	if err != nil {
		return fmt.Errorf("failed to read pair_generate response: %w", err)
	}
	if n == 0 {
		return errors.New("empty pair_generate response")
	}

	var resp struct {
		Success bool   `json:"success"`
		Status  string `json:"status"`
		Error   string `json:"error,omitempty"`
		Data    struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respData[:n], &resp); err != nil {
		return fmt.Errorf("failed to parse pair_generate response: %w", err)
	}
	if !resp.Success {
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return fmt.Errorf("pair_generate failed with status=%s", resp.Status)
	}
	if resp.Data.Code == "" {
		return errors.New("pair_generate returned empty code")
	}

	fmt.Printf("Pairing code for %s: %s\n", appName, resp.Data.Code)
	return nil
}

func pairWithCode(master, client, code, host string) error {
	basePath := getBasePath()

	masterEntry, err := apps.ResolveByName(basePath, master)
	if err != nil {
		return err
	}
	clientEntry, err := apps.ResolveByName(basePath, client)
	if err != nil {
		return err
	}

	network, addr, err := resolveTransportEndpoint(masterEntry)
	if err != nil {
		return fmt.Errorf("failed to resolve transport endpoint for %s: %w", master, err)
	}

	if host != "" && network == "tcp" {
		_, port, splitErr := net.SplitHostPort(addr)
		if splitErr == nil && port != "" {
			addr = net.JoinHostPort(host, port)
		}
	}

	conn, err := net.DialTimeout(network, addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", master, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	payload, _ := json.Marshal(map[string]string{
		"code":   code,
		"client": client,
	})
	msg := transportMessage{
		Type:      "pair_accept",
		Body:      payload,
		Timestamp: time.Now().Unix(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("failed to send pair_accept: %w", err)
	}

	respData := make([]byte, 64*1024)
	n, err := conn.Read(respData)
	if err != nil {
		return fmt.Errorf("failed to read pair_accept response: %w", err)
	}
	if n == 0 {
		return errors.New("empty pair_accept response")
	}

	var resp struct {
		Success bool   `json:"success"`
		Status  string `json:"status"`
		Error   string `json:"error,omitempty"`
		Data    struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respData[:n], &resp); err != nil {
		return fmt.Errorf("failed to parse pair_accept response: %w", err)
	}
	if !resp.Success {
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return fmt.Errorf("pair_accept failed with status=%s", resp.Status)
	}

	serverAddr := host
	if serverAddr == "" {
		serverAddr = master
	}

	socketPath := filepath.Join(masterEntry.Path, "var", "socket", master+".sock")

	pairingInfo := gmcore_transport.PairingInfo{
		AppID:       client,
		GatewayID:   master,
		GatewayAddr: serverAddr,
		SocketPath:  socketPath,
		Secret:      []byte(resp.Data.Secret),
		PairedAt:    time.Now().Unix(),
	}

	keysDir := filepath.Join(clientEntry.Path, "var", "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return fmt.Errorf("failed to create keys directory: %w", err)
	}

	pairingData, err := json.Marshal(pairingInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal pairing info: %w", err)
	}

	pairingPath := filepath.Join(keysDir, "pairing.json")
	if err := os.WriteFile(pairingPath, pairingData, 0600); err != nil {
		return fmt.Errorf("failed to write pairing.json: %w", err)
	}

	fmt.Printf("Application %s paired with %s\n", client, master)
	return nil
}

func unpairApps(master, client string) error {
	basePath := getBasePath()

	masterEntry, err := apps.ResolveByName(basePath, master)
	if err != nil {
		return err
	}
	clientEntry, err := apps.ResolveByName(basePath, client)
	if err != nil {
		return err
	}

	for _, entry := range []apps.Entry{masterEntry, clientEntry} {
		pairingPath := filepath.Join(entry.Path, "var", "keys", "pairing.json")
		if err := os.Remove(pairingPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove pairing for %s: %w", entry.Name, err)
		}
	}

	fmt.Printf("Unpaired %s and %s\n", master, client)
	return nil
}
