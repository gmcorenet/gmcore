package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	gmapps "github.com/gmcorenet/gmcore/internal/apps"
)

func TestResolveTransportEndpointUDS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket endpoint test")
	}

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "config", "transport.yaml"), "server:\n  mode: uds\n  uds:\n    path: var/socket/custom.sock\n")

	entry := gmapps.Entry{Name: "myapp", Path: root}
	network, address, err := resolveTransportEndpoint(entry)
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	if network != "unix" {
		t.Fatalf("unexpected network: %s", network)
	}
	want := filepath.Join(root, "var", "socket", "custom.sock")
	if address != want {
		t.Fatalf("unexpected address: got=%s want=%s", address, want)
	}
}

func TestResolveTransportEndpointTCP(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "config", "transport.yaml"), "server:\n  mode: tcp\n  tcp:\n    host: 127.0.0.1\n    ports: [18080]\n")

	entry := gmapps.Entry{Name: "myapp", Path: root}
	network, address, err := resolveTransportEndpoint(entry)
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	if network != "tcp" {
		t.Fatalf("unexpected network: %s", network)
	}
	if address != "127.0.0.1:18080" {
		t.Fatalf("unexpected address: %s", address)
	}
}

func TestSendLifecycleTransportCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket transport test")
	}

	root := t.TempDir()
	entry := gmapps.Entry{Name: "myapp", Path: root}

	socketDir := filepath.Join(root, "var", "socket")
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}

	socketPath := filepath.Join(socketDir, "myapp.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ready := make(chan struct{})
	go func() {
		close(ready)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		var msg transportMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			return
		}
		if msg.Type != "reload" {
			return
		}

		_, _ = conn.Write([]byte(`{"success":true,"status":"reloaded"}`))
	}()

	<-ready
	ok, err := sendLifecycleTransportCommand(entry, "reload")
	if err != nil {
		t.Fatalf("send lifecycle command: %v", err)
	}
	if !ok {
		t.Fatalf("expected successful response")
	}
}

func TestPIDFileRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "var", "run"), 0755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}

	if err := writePIDFile(root, 12345); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	pid, err := readPIDFile(root)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if pid != 12345 {
		t.Fatalf("unexpected pid: %d", pid)
	}

	running, _, err := pidStatus(root)
	if err != nil {
		t.Fatalf("pid status: %v", err)
	}
	if running {
		t.Fatalf("expected not running process for synthetic pid")
	}

	if err := removePIDFile(root); err != nil {
		t.Fatalf("remove pid file: %v", err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
