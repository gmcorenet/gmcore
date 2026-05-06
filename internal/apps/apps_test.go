package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveByName(t *testing.T) {
	base := t.TempDir()
	mustMkdirApp(t, filepath.Join(base, "myapp"))
	mustMkdirApp(t, filepath.Join(base, "gmcore-api"))

	resolved, err := ResolveByName(base, "myapp")
	if err != nil {
		t.Fatalf("resolve myapp: %v", err)
	}
	if resolved.Dir != "myapp" || resolved.Name != "myapp" {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}

	resolved, err = ResolveByName(base, "api")
	if err != nil {
		t.Fatalf("resolve api: %v", err)
	}
	if resolved.Dir != "gmcore-api" || resolved.Name != "api" {
		t.Fatalf("unexpected prefixed resolution: %+v", resolved)
	}
}

func TestListDeduplicatesAndPrefersPlainDir(t *testing.T) {
	base := t.TempDir()
	mustMkdirApp(t, filepath.Join(base, "billing"))
	mustMkdirApp(t, filepath.Join(base, "gmcore-billing"))

	entries, err := List(base)
	if err != nil {
		t.Fatalf("list apps: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "billing" || entries[0].Dir != "billing" {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
}

func TestDetectFromCWD(t *testing.T) {
	base := t.TempDir()
	appRoot := filepath.Join(base, "gmcore-gateway")
	mustMkdirApp(t, appRoot)
	mustMkdirAll(t, filepath.Join(appRoot, "config"))

	detected := DetectFromCWD(base, filepath.Join(appRoot, "config"))
	if detected != appRoot {
		t.Fatalf("unexpected app root: got=%s want=%s", detected, appRoot)
	}
}

func mustMkdirApp(t *testing.T, path string) {
	t.Helper()
	mustMkdirAll(t, path)
	mustWriteFile(t, filepath.Join(path, "app.yaml"), "app:\n  name: demo\n")
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
