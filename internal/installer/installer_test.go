package installer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallComponent_WithInjectedDownloaderAndExtractor(t *testing.T) {
	dest := t.TempDir()
	inst := New(dest, false)

	var downloaderCalled bool
	var extractorCalled bool

	inst.downloader = func(url, path string) error {
		downloaderCalled = true
		return os.WriteFile(path, []byte("mock-tarball"), 0644)
	}
	inst.extractor = func(src, dst string) error {
		extractorCalled = true
		sourceRoot := filepath.Join(dst, "repo-v1.0.0")
		if err := os.MkdirAll(sourceRoot, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(sourceRoot, "README.md"), []byte("hello"), 0644)
	}

	err := inst.InstallComponent(Component{Repo: "owner/repo", Release: "v1.0.0", Path: "components/repo"})
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if !downloaderCalled {
		t.Fatal("expected downloader to be called")
	}
	if !extractorCalled {
		t.Fatal("expected extractor to be called")
	}

	installedFile := filepath.Join(dest, "components", "repo", "README.md")
	if _, err := os.Stat(installedFile); err != nil {
		t.Fatalf("expected installed file %s: %v", installedFile, err)
	}
}

func TestInstallComponentWithVars_SubstitutesVariables(t *testing.T) {
	dest := t.TempDir()
	inst := NewWithVars(dest, false)

	inst.downloader = func(url, path string) error {
		return os.WriteFile(path, []byte("mock-tarball"), 0644)
	}
	inst.extractor = func(src, dst string) error {
		sourceRoot := filepath.Join(dst, "repo-v1.0.0")
		if err := os.MkdirAll(filepath.Join(sourceRoot, "config"), 0755); err != nil {
			return err
		}
		return os.WriteFile(
			filepath.Join(sourceRoot, "config", "app.yaml"),
			[]byte("name: {{APP_NAME}}\nenv: {{APP_ENV}}\n"),
			0644,
		)
	}

	err := inst.InstallComponentWithVars(
		Component{Repo: "owner/repo", Release: "v1.0.0", Path: "components/repo"},
		map[string]string{"APP_NAME": "gmcore", "APP_ENV": "test"},
	)
	if err != nil {
		t.Fatalf("install with vars failed: %v", err)
	}

	installedFile := filepath.Join(dest, "components", "repo", "config", "app.yaml")
	content, err := os.ReadFile(installedFile)
	if err != nil {
		t.Fatalf("failed reading installed file: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "{{APP_NAME}}") || strings.Contains(text, "{{APP_ENV}}") {
		t.Fatalf("expected placeholders to be replaced, got: %s", text)
	}
	if !strings.Contains(text, "gmcore") || !strings.Contains(text, "test") {
		t.Fatalf("expected substituted values in output, got: %s", text)
	}
}

func TestInstallComponent_DownloadFailure(t *testing.T) {
	dest := t.TempDir()
	inst := New(dest, false)

	inst.downloader = func(url, path string) error {
		return errors.New("network down")
	}

	err := inst.InstallComponent(Component{Repo: "owner/repo", Release: "v1.0.0", Path: "components/repo"})
	if err == nil {
		t.Fatal("expected error when download fails")
	}
}
