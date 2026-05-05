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

	"gopkg.in/yaml.v3"
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
	AppName    string
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
	manifest *AppManifest
}

type AppManifest struct {
	Version   string                 `yaml:"version"`
	Name      string                 `yaml:"name"`
	Framework ManifestComponent      `yaml:"framework"`
	SDKs      []ManifestSDKComponent `yaml:"sdks"`
	Skeleton  ManifestComponent      `yaml:"skeleton"`
}

type ManifestComponent struct {
	Repo    string `yaml:"repo"`
	Release string `yaml:"release"`
	Path    string `yaml:"path"`
}

type ManifestSDKComponent struct {
	Name    string `yaml:"name"`
	Release string `yaml:"release"`
}

func NewUpdateManager(opts *UpdateOptions) *UpdateManager {
	m := &UpdateManager{
		opts:     opts,
		results:  make([]UpdateResult, 0),
		basePath: getBasePath(),
	}

	if opts.AppName != "" {
		m.appPath = filepath.Join(m.basePath, "gmcore-"+opts.AppName)
	} else if detected := detectAppRoot(); detected != "" {
		m.appPath = detected
		parts := strings.Split(filepath.Base(detected), "-")
		if len(parts) >= 2 && parts[0] == "gmcore" {
			m.opts.AppName = strings.Join(parts[1:], "-")
		}
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
	if !strings.HasPrefix(appName, "gmcore-") {
		return ""
	}

	appRoot := filepath.Join(basePath, appName)
	if _, err := os.Stat(appRoot); err != nil {
		return ""
	}

	return appRoot
}

func (m *UpdateManager) Run() error {
	if m.appPath == "" {
		return fmt.Errorf("app name is required. Run from an app directory or use --app=<name>")
	}

	if _, err := os.Stat(m.appPath); os.IsNotExist(err) {
		return fmt.Errorf("app not found: %s", m.opts.AppName)
	}

	manifest, err := m.fetchManifest(m.opts.Version)
	if err != nil {
		return fmt.Errorf("failed to fetch manifest: %w", err)
	}
	m.manifest = manifest

	if m.opts.Verbose {
		fmt.Printf("App: %s\n", m.opts.AppName)
		fmt.Printf("Manifest version: %s\n", manifest.Version)
		fmt.Printf("Update targets: %v\n", m.resolveTargets())
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

func (m *UpdateManager) fetchManifest(version string) (*AppManifest, error) {
	if version == "" || version == "latest" {
		version = "main"
	}

	url := fmt.Sprintf("https://raw.githubusercontent.com/gmcorenet/manifest/%s/app/v1.0.yaml", version)

	if m.opts.Verbose {
		fmt.Printf("Fetching manifest from: %s\n", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("manifest not found for version: %s", version)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch manifest: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	return parseManifest(data)
}

func parseManifest(data []byte) (*AppManifest, error) {
	var manifest AppManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if manifest.Version == "" {
		manifest.Version = "1.0"
	}

	return &manifest, nil
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
	varDir := filepath.Join("/var", "gmcore-"+m.opts.AppName, "backups")
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
		sourcePath = filepath.Join(m.appPath, m.manifest.Framework.Path)
		if sourcePath == "" {
			sourcePath = filepath.Join(m.appPath, "vendor", "framework")
		}
	case TargetSDKs:
		sourcePath = filepath.Join(m.appPath, "vendor", "sdks")
	case TargetSkeleton:
		sourcePath = filepath.Join(m.appPath, m.manifest.Skeleton.Path)
		if sourcePath == "" || sourcePath == "." {
			sourcePath = m.appPath
		}
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

	component := m.manifest.Framework
	version := m.resolveVersion(component.Release)

	currentVersion := m.getCurrentVersion()
	result.From = currentVersion
	result.To = version

	if m.opts.Verbose {
		fmt.Printf("  Framework: %s -> %s (repo: %s)\n", currentVersion, version, component.Repo)
	}

	destPath := filepath.Join(m.appPath, component.Path)
	if destPath == "" || destPath == "." {
		destPath = filepath.Join(m.appPath, "vendor", "framework")
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

	if err := m.downloadAndExtract(component.Repo, version, destPath); err != nil {
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

	sdksToUpdate := m.opts.SDKs
	if len(sdksToUpdate) == 0 {
		for _, sdk := range m.manifest.SDKs {
			sdksToUpdate = append(sdksToUpdate, sdk.Name)
		}
	}

	var baseVersion string
	if len(m.manifest.SDKs) > 0 {
		baseVersion = m.manifest.SDKs[0].Release
	}
	version := m.resolveVersion(baseVersion)
	result.From = "previous"
	result.To = version

	if m.opts.Verbose {
		fmt.Printf("  SDKs base version: %s\n", version)
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
	for _, sdkName := range sdksToUpdate {
		var sdkRelease string

		for _, sdk := range m.manifest.SDKs {
			if sdk.Name == sdkName {
				sdkRelease = sdk.Release
				break
			}
		}

		if sdkRelease == "" {
			sdkRelease = baseVersion
		}

		sdkVersion := m.resolveVersion(sdkRelease)

		sdkFullPath := filepath.Join(m.appPath, "vendor", "sdks", sdkName)
		if m.opts.Verbose {
			fmt.Printf("  Updating SDK: %s @ %s\n", sdkName, sdkVersion)
		}

		if err := m.downloadAndExtract("gmcorenet/sdk", sdkVersion, sdkFullPath); err != nil {
			fmt.Printf("  Warning: failed to update %s: %v\n", sdkName, err)
			continue
		}
		successCount++
	}

	if successCount == 0 && len(sdksToUpdate) > 0 {
		result.Error = fmt.Errorf("no SDKs updated successfully")
		return result
	}

	result.Success = true
	fmt.Printf("SDKs updated (%d/%d): %s\n", successCount, len(sdksToUpdate), version)
	if result.BackupPath != "" {
		fmt.Printf("  Backup: %s\n", result.BackupPath)
	}
	return result
}

func (m *UpdateManager) updateSkeleton() UpdateResult {
	result := UpdateResult{Target: TargetSkeleton}

	component := m.manifest.Skeleton
	version := m.resolveVersion(component.Release)

	currentVersion := m.getCurrentVersion()
	result.From = currentVersion
	result.To = version

	if m.opts.Verbose {
		fmt.Printf("  Skeleton: %s -> %s (repo: %s)\n", currentVersion, version, component.Repo)
	}

	destPath := filepath.Join(m.appPath, component.Path)
	if destPath == "" || destPath == "." {
		destPath = m.appPath
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

	if err := m.downloadAndExtract(component.Repo, version, destPath); err != nil {
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
	result.To = m.manifest.Version

	fmt.Printf("App manifest version: %s\n", m.manifest.Version)

	result.Success = true
	return result
}

func (m *UpdateManager) resolveVersion(release string) string {
	if release == "" || release == "latest" {
		return "main"
	}

	if strings.HasPrefix(release, "v") || strings.HasPrefix(release, "1.") {
		return release
	}

	return "v" + release
}

func (m *UpdateManager) getCurrentVersion() string {
	versionFile := filepath.Join(m.appPath, "VERSION")
	if data, err := os.ReadFile(versionFile); err == nil {
		return strings.TrimSpace(string(data))
	}
	return "unknown"
}

func (m *UpdateManager) downloadAndExtract(repo, version, destPath string) error {
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/archive/refs/tags/%s.tar.gz",
		repo, version,
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
				restorePath = filepath.Join(m.appPath, m.manifest.Framework.Path)
				if restorePath == "" {
					restorePath = filepath.Join(m.appPath, "vendor", "framework")
				}
			case TargetSDKs:
				restorePath = filepath.Join(m.appPath, "vendor", "sdks")
			case TargetSkeleton:
				restorePath = filepath.Join(m.appPath, m.manifest.Skeleton.Path)
				if restorePath == "" || restorePath == "." {
					restorePath = m.appPath
				}
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

func getBasePath() string {
	switch runtime.GOOS {
	case "windows":
		return "C:\\ProgramData\\gmcore"
	default:
		return "/opt/gmcore"
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