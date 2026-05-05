package update

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type UpdateTarget string

const (
	TargetFramework UpdateTarget = "framework"
	TargetSDKs     UpdateTarget = "sdks"
	TargetSkeleton UpdateTarget = "skeleton"
	TargetApp      UpdateTarget = "app"
	TargetAll      UpdateTarget = "all"
)

type UpdateOptions struct {
	Target     UpdateTarget
	Version    string
	SDKs       []string
	AppPath    string
	Rollback   bool
	Verbose    bool
	SkipVerify bool
}

type UpdateResult struct {
	Target     UpdateTarget
	From       string
	To         string
	Success    bool
	Error      error
	Rollback   bool
	BackupPath string
}

type UpdateManager struct {
	opts     *UpdateOptions
	results  []UpdateResult
	basePath string
	appPath  string
}

func NewUpdateManager(opts *UpdateOptions) *UpdateManager {
	m := &UpdateManager{
		opts:     opts,
		results:  make([]UpdateResult, 0),
		basePath: getBasePath(),
	}

	if opts.AppPath != "" {
		m.appPath = opts.AppPath
	} else if detected := detectAppRoot(); detected != "" {
		m.appPath = detected
	} else {
		m.appPath = ""
	}

	return m
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

func (m *UpdateManager) Run() error {
	if m.appPath == "" {
		return fmt.Errorf("app path is required. Run from an app directory or use --app=<path>")
	}

	if m.opts.Verbose {
		fmt.Printf("App: %s\n", m.appPath)
		fmt.Printf("Update targets: %v\n", m.resolveTargets())
		fmt.Printf("Version: %s\n", m.opts.Version)
		fmt.Printf("Rollback on failure: %v\n", m.opts.Rollback)
		fmt.Println("")
	}

	targets := m.resolveTargets()

	for i, target := range targets {
		if m.opts.Verbose {
			fmt.Printf("[%d/%d] Updating %s...\n", i+1, len(targets), target)
		}

		result := m.updateTarget(target)
		m.results = append(m.results, result)

		if !result.Success && m.opts.Rollback {
			fmt.Printf("Error updating %s: %v\n", target, result.Error)
			fmt.Println("Rolling back...")
			if err := m.rollback(target); err != nil {
				fmt.Printf("Rollback failed: %v\n", err)
			}
			return result.Error
		}

		if !result.Success {
			fmt.Printf("Warning: failed to update %s: %v\n", target, result.Error)
		}
	}

	return m.printSummary()
}

func (m *UpdateManager) resolveTargets() []UpdateTarget {
	switch m.opts.Target {
	case TargetAll:
		return []UpdateTarget{TargetFramework, TargetSDKs, TargetSkeleton}
	case TargetFramework, TargetSDKs, TargetSkeleton, TargetApp:
		return []UpdateTarget{m.opts.Target}
	default:
		return []UpdateTarget{m.opts.Target}
	}
}

func (m *UpdateManager) updateTarget(target UpdateTarget) UpdateResult {
	result := UpdateResult{Target: target}

	switch target {
	case TargetFramework:
		return m.updateFramework()
	case TargetSDKs:
		return m.updateSDKs()
	case TargetSkeleton:
		return m.updateSkeleton()
	case TargetApp:
		return m.updateApp()
	default:
		result.Error = fmt.Errorf("unknown target: %s", target)
		return result
	}
}

func (m *UpdateManager) getBackupDir() string {
	appName := filepath.Base(m.appPath)
	varDir := filepath.Join("/var", "gmcore-"+appName, "backups")
	os.MkdirAll(varDir, 0755)
	return varDir
}

func (m *UpdateManager) createBackup(target UpdateTarget, version string) (string, error) {
	backupDir := m.getBackupDir()
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s_%s_%s.tar.gz", target, version, timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	var sourcePath string
	switch target {
	case TargetFramework:
		sourcePath = filepath.Join(m.appPath, "vendor", "framework")
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			sourcePath = filepath.Join(m.appPath, "packages", "framework")
		}
	case TargetSDKs:
		sourcePath = filepath.Join(m.appPath, "vendor", "sdks")
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			sourcePath = filepath.Join(m.appPath, "packages", "sdks")
		}
	case TargetSkeleton:
		sourcePath = m.appPath
	case TargetApp:
		sourcePath = m.appPath
	default:
		return "", fmt.Errorf("unknown target for backup: %s", target)
	}

	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return "", nil
	}

	if err := createTarGz(sourcePath, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	return backupPath, nil
}

func (m *UpdateManager) updateFramework() UpdateResult {
	result := UpdateResult{Target: TargetFramework}

	version, err := m.resolveVersion("gmcorenet", "framework", m.opts.Version)
	if err != nil {
		result.Error = err
		return result
	}

	currentVersion := m.getCurrentVersion("framework")
	result.From = currentVersion
	result.To = version

	if m.opts.Verbose {
		fmt.Printf("  Framework: %s -> %s\n", currentVersion, version)
	}

	frameworkPath := filepath.Join(m.appPath, "vendor", "framework")
	if _, err := os.Stat(frameworkPath); os.IsNotExist(err) {
		frameworkPath = filepath.Join(m.appPath, "packages", "framework")
	}

	if m.opts.Rollback {
		backupPath, err := m.createBackup(TargetFramework, currentVersion)
		if err != nil {
			fmt.Printf("  Warning: failed to create backup: %v\n", err)
		} else {
			result.BackupPath = backupPath
			if m.opts.Verbose {
				fmt.Printf("  Backup created: %s\n", backupPath)
			}
		}
	}

	if err := m.downloadAndExtract("gmcorenet/framework", version, frameworkPath); err != nil {
		result.Error = err
		return result
	}

	result.Success = true
	fmt.Printf("Framework updated: %s -> %s\n", currentVersion, version)
	if result.BackupPath != "" {
		fmt.Printf("  Backup: %s\n", result.BackupPath)
	}
	return result
}

func (m *UpdateManager) updateSDKs() UpdateResult {
	result := UpdateResult{Target: TargetSDKs}

	if len(m.opts.SDKs) == 0 {
		m.opts.SDKs = getAllSDKs()
	}

	version, err := m.resolveVersion("gmcorenet", "sdk", m.opts.Version)
	if err != nil {
		result.Error = err
		return result
	}

	result.From = "previous"
	result.To = version

	sdkPath := filepath.Join(m.appPath, "vendor", "sdks")
	if _, err := os.Stat(sdkPath); os.IsNotExist(err) {
		sdkPath = filepath.Join(m.appPath, "packages", "sdks")
	}

	if m.opts.Rollback {
		backupPath, err := m.createBackup(TargetSDKs, "previous")
		if err != nil {
			fmt.Printf("  Warning: failed to create backup: %v\n", err)
		} else {
			result.BackupPath = backupPath
			if m.opts.Verbose {
				fmt.Printf("  Backup created: %s\n", backupPath)
			}
		}
	}

	successCount := 0
	for _, sdk := range m.opts.SDKs {
		if m.opts.Verbose {
			fmt.Printf("  Updating SDK: %s\n", sdk)
		}

		sdkFullPath := filepath.Join(sdkPath, sdk)
		if err := m.downloadAndExtract(fmt.Sprintf("gmcorenet/sdk"), version, sdkFullPath); err != nil {
			fmt.Printf("  Warning: failed to update %s: %v\n", sdk, err)
			continue
		}
		successCount++
	}

	if successCount == 0 {
		result.Error = fmt.Errorf("no SDKs updated successfully")
		return result
	}

	result.Success = true
	fmt.Printf("SDKs updated (%d/%d): %s\n", successCount, len(m.opts.SDKs), version)
	if result.BackupPath != "" {
		fmt.Printf("  Backup: %s\n", result.BackupPath)
	}
	return result
}

func (m *UpdateManager) updateSkeleton() UpdateResult {
	result := UpdateResult{Target: TargetSkeleton}

	version, err := m.resolveVersion("gmcorenet", "skeleton", m.opts.Version)
	if err != nil {
		result.Error = err
		return result
	}

	currentVersion := m.getCurrentVersion("skeleton")
	result.From = currentVersion
	result.To = version

	if m.opts.Verbose {
		fmt.Printf("  Skeleton: %s -> %s\n", currentVersion, version)
	}

	if m.opts.Rollback {
		backupPath, err := m.createBackup(TargetSkeleton, currentVersion)
		if err != nil {
			fmt.Printf("  Warning: failed to create backup: %v\n", err)
		} else {
			result.BackupPath = backupPath
			if m.opts.Verbose {
				fmt.Printf("  Backup created: %s\n", backupPath)
			}
		}
	}

	if err := m.downloadAndExtract("gmcorenet/skeleton", version, m.appPath); err != nil {
		result.Error = err
		return result
	}

	result.Success = true
	fmt.Printf("Skeleton updated: %s -> %s\n", currentVersion, version)
	if result.BackupPath != "" {
		fmt.Printf("  Backup: %s\n", result.BackupPath)
	}
	return result
}

func (m *UpdateManager) updateApp() UpdateResult {
	result := UpdateResult{Target: TargetApp}

	if _, err := os.Stat(m.appPath); os.IsNotExist(err) {
		result.Error = fmt.Errorf("app not found at %s", m.appPath)
		return result
	}

	result.From = "current"
	result.To = m.opts.Version

	if m.opts.Version != "" && m.opts.Version != "latest" {
		fmt.Printf("Updating app to version: %s\n", m.opts.Version)
	}

	result.Success = true
	return result
}

func (m *UpdateManager) resolveVersion(owner, repo, version string) (string, error) {
	if version == "" || version == "latest" {
		tag, err := m.getLatestTag(owner, repo)
		if err != nil {
			return "main", err
		}
		return tag, nil
	}

	if strings.HasPrefix(version, "v") || strings.HasPrefix(version, "1.") {
		return version, nil
	}

	return "v" + version, nil
}

func (m *UpdateManager) getLatestTag(owner, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to get latest release: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	bodyStr := string(body)
	for _, line := range strings.Split(bodyStr, "\n") {
		if strings.Contains(line, `"tag_name"`) {
			parts := strings.Split(line, `"`)
			if len(parts) >= 4 {
				return parts[3], nil
			}
		}
	}

	return "v1.0.0", nil
}

func (m *UpdateManager) getCurrentVersion(component string) string {
	switch component {
	case "framework":
		versionFile := filepath.Join(m.appPath, "vendor", "framework", "VERSION")
		if data, err := os.ReadFile(versionFile); err == nil {
			return strings.TrimSpace(string(data))
		}
	case "skeleton":
		versionFile := filepath.Join(m.appPath, "VERSION")
		if data, err := os.ReadFile(versionFile); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return "unknown"
}

func (m *UpdateManager) downloadAndExtract(repo, version, destPath string) error {
	owner, name := parseRepo(repo)
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		owner, name, version,
	)

	tmpDir, err := os.MkdirTemp("", "gmcore-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "component.tar.gz")

	if m.opts.Verbose {
		fmt.Printf("  Downloading from %s\n", tarballURL)
	}

	if err := downloadFile(tarballURL, tarballPath); err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	extractPath := filepath.Join(tmpDir, "extracted")
	if err := extractTarGz(tarballPath, extractPath); err != nil {
		return fmt.Errorf("failed to extract: %w", err)
	}

	entries, err := os.ReadDir(extractPath)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("failed to read extracted content")
	}

	sourceDir := filepath.Join(extractPath, entries[0].Name())

	if err := os.RemoveAll(destPath); err != nil {
		fmt.Printf("  Warning: failed to remove old version: %v\n", err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := copyDir(sourceDir, destPath); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return nil
}

func (m *UpdateManager) rollback(target UpdateTarget) error {
	for _, result := range m.results {
		if result.Target == target && result.Success && result.BackupPath != "" {
			fmt.Printf("Rolling back %s from %s to %s\n", target, result.To, result.From)
			fmt.Printf("  Restoring from: %s\n", result.BackupPath)

			var restorePath string
			switch target {
			case TargetFramework:
				restorePath = filepath.Join(m.appPath, "vendor", "framework")
				if _, err := os.Stat(restorePath); os.IsNotExist(err) {
					restorePath = filepath.Join(m.appPath, "packages", "framework")
				}
			case TargetSDKs:
				restorePath = filepath.Join(m.appPath, "vendor", "sdks")
				if _, err := os.Stat(restorePath); os.IsNotExist(err) {
					restorePath = filepath.Join(m.appPath, "packages", "sdks")
				}
			case TargetSkeleton:
				restorePath = m.appPath
			default:
				return fmt.Errorf("unknown target for rollback: %s", target)
			}

			tmpDir, err := os.MkdirTemp("", "gmcore-rollback-*")
			if err != nil {
				return fmt.Errorf("failed to create temp dir: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			extractPath := filepath.Join(tmpDir, "restored")
			if err := extractTarGz(result.BackupPath, extractPath); err != nil {
				return fmt.Errorf("failed to extract backup: %w", err)
			}

			entries, err := os.ReadDir(extractPath)
			if err != nil || len(entries) == 0 {
				return fmt.Errorf("failed to read backup content")
			}

			sourceDir := filepath.Join(extractPath, entries[0].Name())

			if err := os.RemoveAll(restorePath); err != nil {
				fmt.Printf("  Warning: failed to remove current version: %v\n", err)
			}

			if err := copyDir(sourceDir, restorePath); err != nil {
				return fmt.Errorf("failed to restore: %w", err)
			}

			fmt.Printf("Rollback completed successfully\n")
			return nil
		}
	}
	return fmt.Errorf("no successful update with backup found to rollback")
}

func (m *UpdateManager) printSummary() error {
	fmt.Println("")
	fmt.Println("Update Summary:")
	fmt.Println("===============")

	var failed []string
	var succeeded []string

	for _, r := range m.results {
		status := "OK"
		if !r.Success {
			status = "FAILED"
			failed = append(failed, string(r.Target))
		} else {
			succeeded = append(succeeded, string(r.Target))
		}
		fmt.Printf("  %s: %s -> %s [%s]\n", r.Target, r.From, r.To, status)
		if r.BackupPath != "" {
			fmt.Printf("    Backup: %s\n", r.BackupPath)
		}
	}

	fmt.Println("")
	if len(failed) > 0 {
		fmt.Printf("Failed targets: %v\n", failed)
		return fmt.Errorf("update partially failed")
	}

	fmt.Println("All targets updated successfully!")
	return nil
}

func parseRepo(repo string) (owner, name string) {
	parts := strings.Split(repo, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", repo
}

func getBasePath() string {
	switch runtime.GOOS {
	case "windows":
		return "C:\\ProgramData\\gmcore"
	default:
		return "/opt/gmcore"
	}
}

func getAllSDKs() []string {
	return []string{
		"gmcore-asset",
		"gmcore-bundle",
		"gmcore-cache",
		"gmcore-cert",
		"gmcore-config",
		"gmcore-console",
		"gmcore-crud",
		"gmcore-error",
		"gmcore-events",
		"gmcore-expression",
		"gmcore-form",
		"gmcore-httpclient",
		"gmcore-i18n",
		"gmcore-lifecycle",
		"gmcore-lock",
		"gmcore-log",
		"gmcore-mailer",
		"gmcore-messenger",
		"gmcore-migrations",
		"gmcore-notifier",
		"gmcore-orm",
		"gmcore-ratelimit",
		"gmcore-resolver",
		"gmcore-response",
		"gmcore-router",
		"gmcore-scheduler",
		"gmcore-security",
		"gmcore-serializer",
		"gmcore-session",
		"gmcore-settings",
		"gmcore-templating",
		"gmcore-uid",
		"gmcore-validation",
		"gmcore-view",
		"gmcore-webhook",
		"gmcore-workflow",
	}
}

func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	return err
}

func extractTarGz(tarballPath, destPath string) error {
	file, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destPath, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, 0644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

func createTarGz(sourcePath, destPath string) error {
	file, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzw := gzip.NewWriter(file)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	return filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(filepath.Dir(sourcePath), path)
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		_, err = io.Copy(tw, srcFile)
		return err
	})
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(path, destPath)
	})
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}