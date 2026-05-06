package update

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gmcorenet/gmcore/internal/apps"
	"github.com/gmcorenet/gmcore/internal/installer"
	"github.com/gmcorenet/gmcore/internal/manifest"
)

type UpdateTarget string

const (
	TargetFramework UpdateTarget = "framework"
	TargetSDKs      UpdateTarget = "sdks"
	TargetSkeleton  UpdateTarget = "skeleton"
	TargetApp       UpdateTarget = "app"
	TargetAll       UpdateTarget = "all"
)

type UpdateOptions struct {
	Target   UpdateTarget
	Version  string
	SDKs     []string
	AppName  string
	Rollback bool
	Verbose  bool
	Force    bool
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
	appName  string
	mf       *manifest.Manifest
}

func NewUpdateManager(opts *UpdateOptions) *UpdateManager {
	m := &UpdateManager{
		opts:     opts,
		results:  make([]UpdateResult, 0),
		basePath: apps.BasePath(),
	}

	if opts.AppName != "" {
		resolved, err := apps.ResolveByName(m.basePath, opts.AppName)
		if err == nil {
			m.appPath = resolved.Path
			m.appName = resolved.Name
		} else {
			m.appPath = filepath.Join(m.basePath, opts.AppName)
			m.appName = apps.NormalizeName(opts.AppName)
		}
	} else if detected := detectAppRoot(); detected != "" {
		m.appPath = detected
		m.appName = apps.NormalizeName(filepath.Base(detected))
	}

	return m
}

func detectAppRoot() string {
	return apps.DetectFromCWD(apps.BasePath(), "")
}

func (m *UpdateManager) Run() error {
	if m.appPath == "" {
		return fmt.Errorf("app name is required. Run from an app directory or use --app=<name>")
	}

	if _, err := os.Stat(m.appPath); os.IsNotExist(err) {
		return fmt.Errorf("app not found: %s", m.appName)
	}

	mf, err := manifest.Fetch("gmcorenet", "manifest", m.opts.Version)
	if err != nil {
		return fmt.Errorf("failed to fetch manifest: %w", err)
	}
	m.mf = mf

	if m.opts.Verbose {
		fmt.Printf("App: %s\n", m.appName)
		fmt.Printf("Manifest version: %s\n", mf.Version)
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
	varDir := filepath.Join(m.appPath, "var", "backups")
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
		sourcePath = filepath.Join(m.appPath, m.mf.Framework.Path)
	case TargetSDKs:
		sourcePath = filepath.Join(m.appPath, "vendor", "sdks")
	case TargetSkeleton:
		skeletonPath := m.mf.Skeleton.Path
		if skeletonPath == "" || skeletonPath == "." {
			sourcePath = m.appPath
		} else {
			sourcePath = filepath.Join(m.appPath, skeletonPath)
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

	framework := m.mf.GetFramework()
	currentVersion := m.getCurrentVersion()

	if m.opts.Verbose {
		fmt.Printf("  Framework: %s -> %s (repo: %s)\n", currentVersion, framework.Release, framework.Repo)
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

	inst := installer.New(m.appPath, m.opts.Verbose)
	if err := inst.InstallComponent(installer.Component{
		Repo:    framework.Repo,
		Release: framework.Release,
		Path:    framework.Path,
	}); err != nil {
		result.Error = err
		return result
	}

	result.From = currentVersion
	result.To = framework.Release
	result.Success = true
	fmt.Printf("Framework updated: %s -> %s\n", currentVersion, framework.Release)
	if result.BackupPath != "" {
		fmt.Printf("  Backup: %s\n", result.BackupPath)
	}
	return result
}

func (m *UpdateManager) updateSDKs() UpdateResult {
	result := UpdateResult{Target: TargetSDKs}
	effectiveSDKs := m.mf.EffectiveSDKs(m.appName)

	sdksToUpdate := m.opts.SDKs
	if len(sdksToUpdate) == 0 {
		for _, sdk := range effectiveSDKs {
			sdksToUpdate = append(sdksToUpdate, sdk.Name)
		}
	}

	result.From = "previous"
	if len(effectiveSDKs) > 0 {
		result.To = effectiveSDKs[0].Release
	} else {
		result.To = "unknown"
	}

	if m.opts.Verbose {
		fmt.Printf("  SDKs base version: %s\n", result.To)
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

	inst := installer.New(m.appPath, m.opts.Verbose)
	successCount := 0

	for _, sdkName := range sdksToUpdate {
		var sdkRelease string

		for _, sdk := range effectiveSDKs {
			if sdk.Name == sdkName {
				sdkRelease = sdk.Release
				break
			}
		}

		if sdkRelease == "" {
			sdkRelease = result.To
		}

		sdkPath := "vendor/sdks/" + sdkName

		if m.opts.Verbose {
			fmt.Printf("  Updating SDK: %s @ %s\n", sdkName, sdkRelease)
		}

		if err := inst.InstallComponent(installer.Component{
			Repo:    "gmcorenet/" + sdkName,
			Release: sdkRelease,
			Path:    sdkPath,
		}); err != nil {
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
	fmt.Printf("SDKs updated (%d/%d)\n", successCount, len(sdksToUpdate))
	if result.BackupPath != "" {
		fmt.Printf("  Backup: %s\n", result.BackupPath)
	}
	return result
}

func (m *UpdateManager) updateSkeleton() UpdateResult {
	result := UpdateResult{Target: TargetSkeleton}

	skeleton := m.mf.GetSkeleton()
	currentVersion := m.getCurrentVersion()

	if m.opts.Verbose {
		fmt.Printf("  Skeleton: %s -> %s (repo: %s)\n", currentVersion, skeleton.Release, skeleton.Repo)
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

	inst := installer.NewWithProtection(m.appPath, m.opts.Verbose, installer.DefaultProtectedPatterns)
	skeletonPath := skeleton.Path
	if skeletonPath == "" {
		skeletonPath = "."
	}

	mergeResult, err := inst.InstallComponentMerge(installer.Component{
		Repo:    skeleton.Repo,
		Release: skeleton.Release,
		Path:    skeletonPath,
	}, m.opts.Verbose, m.opts.Force)
	if err != nil {
		result.Error = err
		return result
	}

	result.From = currentVersion
	result.To = skeleton.Release
	result.Success = true
	fmt.Printf("Skeleton updated: %s -> %s\n", currentVersion, skeleton.Release)
	if len(mergeResult.Merged) > 0 {
		fmt.Printf("  Merged %d file(s): %v\n", len(mergeResult.Merged), mergeResult.Merged)
	}
	if len(mergeResult.Skipped) > 0 {
		fmt.Printf("  Skipped %d file(s)\n", len(mergeResult.Skipped))
	}
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
	result.To = "latest"
	result.Success = true
	fmt.Printf("App up to date\n")
	return result
}

func (m *UpdateManager) getCurrentVersion() string {
	versionFile := filepath.Join(m.appPath, "VERSION")
	if data, err := os.ReadFile(versionFile); err == nil {
		return strings.TrimSpace(string(data))
	}
	return "unknown"
}

func (m *UpdateManager) rollback(target UpdateTarget) error {
	for _, result := range m.results {
		if result.Target == target && result.Success && result.BackupPath != "" {
			fmt.Printf("Rolling back %s from %s to %s\n", target, result.To, result.From)
			fmt.Printf("  Restoring from: %s\n", result.BackupPath)

			var restorePath string
			switch target {
			case TargetFramework:
				restorePath = filepath.Join(m.appPath, m.mf.Framework.Path)
			case TargetSDKs:
				restorePath = filepath.Join(m.appPath, "vendor", "sdks")
			case TargetSkeleton:
				skeletonPath := m.mf.Skeleton.Path
				if skeletonPath == "" || skeletonPath == "." {
					restorePath = m.appPath
				} else {
					restorePath = filepath.Join(m.appPath, skeletonPath)
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
	return apps.BasePath()
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

		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destPath)+string(os.PathSeparator)) && target != filepath.Clean(destPath) {
			return fmt.Errorf("path traversal detected: %s", header.Name)
		}

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

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}
