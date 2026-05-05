package installer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gmcorenet/gmcore/internal/download"
)

type Component struct {
	Repo    string
	Release string
	Path    string
}

type Installer struct {
	destPath          string
	verbose           bool
	protectedPatterns []string
}

func New(destPath string, verbose bool) *Installer {
	return &Installer{
		destPath: destPath,
		verbose:  verbose,
	}
}

func NewWithProtection(destPath string, verbose bool, protectedPatterns []string) *Installer {
	return &Installer{
		destPath:          destPath,
		verbose:           verbose,
		protectedPatterns: protectedPatterns,
	}
}

var DefaultProtectedPatterns = []string{
	".env",
	".env.local",
	".env.*",
	"config/",
	"var/",
	"data/",
	"storage/",
	"*.yaml",
	"*.yml",
	"*.json",
	"composer.json",
	"go.mod",
	"go.sum",
}

func (i *Installer) InstallComponent(comp Component) error {
	release, err := i.resolveRelease(comp.Release, comp.Repo)
	if err != nil {
		return err
	}

	if i.verbose {
		fmt.Printf("Installing %s @ %s...\n", comp.Repo, release)
	}

	owner, name := parseRepo(comp.Repo)
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		owner, name, release,
	)

	if i.verbose {
		fmt.Printf("  Downloading from %s\n", tarballURL)
	}

	tmpDir, err := os.MkdirTemp("", "gmcore-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "component.tar.gz")

	if err := download.File(tarballURL, tarballPath); err != nil {
		return fmt.Errorf("failed to download %s: %w", comp.Repo, err)
	}

	extractPath := filepath.Join(tmpDir, "extracted")
	if err := extractTarGz(tarballPath, extractPath); err != nil {
		return fmt.Errorf("failed to extract %s: %w", comp.Repo, err)
	}

	entries, err := os.ReadDir(extractPath)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("failed to read extracted content for %s", comp.Repo)
	}

	sourceDir := filepath.Join(extractPath, entries[0].Name())
	destDir := filepath.Join(i.destPath, comp.Path)

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", comp.Path, err)
	}

	if err := copyDir(sourceDir, destDir); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", comp.Repo, comp.Path, err)
	}

	if i.verbose {
		fmt.Printf("  Installed %s to %s\n", comp.Repo, comp.Path)
	}

	return nil
}

func (i *Installer) InstallComponentProtected(comp Component, verbose bool) error {
	release, err := i.resolveRelease(comp.Release, comp.Repo)
	if err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Installing protected %s @ %s...\n", comp.Repo, release)
	}

	owner, name := parseRepo(comp.Repo)
	tarballURL := fmt.Sprintf(
		"https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		owner, name, release,
	)

	if verbose {
		fmt.Printf("  Downloading from %s\n", tarballURL)
	}

	tmpDir, err := os.MkdirTemp("", "gmcore-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, "component.tar.gz")

	if err := download.File(tarballURL, tarballPath); err != nil {
		return fmt.Errorf("failed to download %s: %w", comp.Repo, err)
	}

	extractPath := filepath.Join(tmpDir, "extracted")
	if err := extractTarGz(tarballPath, extractPath); err != nil {
		return fmt.Errorf("failed to extract %s: %w", comp.Repo, err)
	}

	entries, err := os.ReadDir(extractPath)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("failed to read extracted content for %s", comp.Repo)
	}

	sourceDir := filepath.Join(extractPath, entries[0].Name())
	destDir := filepath.Join(i.destPath, comp.Path)

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", comp.Path, err)
	}

	skippedFiles := []string{}
	for _, pattern := range i.protectedPatterns {
		if verbose {
			fmt.Printf("  Protected pattern: %s\n", pattern)
		}
	}

	if err := copyDirProtected(sourceDir, destDir, i.protectedPatterns, &skippedFiles); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", comp.Repo, comp.Path, err)
	}

	if verbose && len(skippedFiles) > 0 {
		fmt.Printf("  Skipped %d protected files:\n", len(skippedFiles))
		for _, f := range skippedFiles {
			if len(f) < 50 {
				fmt.Printf("    - %s\n", f)
			}
		}
	}

	if verbose {
		fmt.Printf("  Installed %s to %s (with protection)\n", comp.Repo, comp.Path)
	}

	return nil
}

func (i *Installer) resolveRelease(release, repo string) (string, error) {
	if release == "" || release == "latest" {
		tag, err := i.getLatestTag(repo)
		if err != nil {
			return "main", nil
		}
		return tag, nil
	}

	if strings.HasPrefix(release, "v") || strings.HasPrefix(release, "1.") {
		return release, nil
	}

	return "v" + release, nil
}

func (i *Installer) getLatestTag(repo string) (string, error) {
	owner, name := parseRepo(repo)
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, name)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to get latest release: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"tag_name"`) {
			parts := strings.Split(line, `"`)
			if len(parts) >= 4 {
				return parts[3], nil
			}
		}
	}

	return "", fmt.Errorf("tag_name not found in response")
}

func parseRepo(repo string) (owner, name string) {
	parts := strings.Split(repo, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", repo
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

func copyDirProtected(src, dst string, patterns []string, skippedFiles *[]string) error {
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
			if _, err := os.Stat(destPath); os.IsNotExist(err) {
				return os.MkdirAll(destPath, info.Mode())
			}
			return nil
		}

		if isProtected(relPath, patterns) {
			if _, err := os.Stat(destPath); err == nil {
				*skippedFiles = append(*skippedFiles, relPath)
				return nil
			}
		}

		return copyFile(path, destPath)
	})
}

func isProtected(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(relPath, pattern) {
			return true
		}
	}
	return false
}

func matchPattern(path, pattern string) bool {
	if pattern == "" {
		return false
	}

	if strings.HasSuffix(pattern, "/") {
		dirPrefix := strings.TrimSuffix(pattern, "/")
		if strings.HasPrefix(path, dirPrefix) {
			return true
		}
		return false
	}

	if strings.HasPrefix(pattern, "*.") {
		ext := strings.TrimPrefix(pattern, "*.")
		return strings.HasSuffix(path, "."+ext)
	}

	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(path, parts[0]) && strings.HasSuffix(path, parts[1])
		}
		return false
	}

	return path == pattern || path == strings.TrimPrefix(pattern, "./")
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