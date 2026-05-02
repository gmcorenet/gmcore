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

	"github.com/gmcore/internal/download"
)

type Component struct {
	Repo    string
	Release string
	Path    string
}

type Installer struct {
	destPath string
	verbose  bool
}

func New(destPath string, verbose bool) *Installer {
	return &Installer{
		destPath: destPath,
		verbose:  verbose,
	}
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