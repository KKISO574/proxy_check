package probe

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycheck/backend/internal/config"
	"proxycheck/backend/internal/storage"
)

func TestMihomoManagerPrepareBuildsRuntimeConfigForTask(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "task.yaml")
	if err := os.WriteFile(source, []byte(`proxies:
  - name: node-a
    type: ss
    server: a.example.com
    port: 443
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	t.Setenv("MIHOMO_SECRET", "secret")
	settings := config.DefaultSettings()
	settings.Mihomo.WorkDir = filepath.Join(dir, "mihomo")

	manager := NewMihomoManager(settings)
	task := &storage.Task{ID: 1, ConfigPath: source}
	listenerPort := 20001
	if err := manager.Prepare(task, []storage.Node{{Name: "node-a", ListenerPort: &listenerPort}}); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(settings.Mihomo.WorkDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	text := string(content)
	for _, want := range []string{"secret: secret", "proxy: node-a", "port: 20001"} {
		if !strings.Contains(text, want) {
			t.Fatalf("runtime config missing %q\n%s", want, text)
		}
	}
}

func TestMihomoManagerReuseSignatureChangesWhenConfigOrListenersChange(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "task.yaml")
	if err := os.WriteFile(source, []byte(`proxies:
  - name: node-a
    type: ss
    server: a.example.com
    port: 443
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	t.Setenv("MIHOMO_SECRET", "secret")
	settings := config.DefaultSettings()
	settings.Mihomo.WorkDir = filepath.Join(dir, "mihomo")
	manager := NewMihomoManager(settings)
	task := &storage.Task{ID: 1, ConfigPath: source}
	listenerPort := 20001

	signatureA, err := manager.runtimeSignature(task, []storage.Node{{Name: "node-a", ListenerPort: &listenerPort}})
	if err != nil {
		t.Fatalf("runtime signature A: %v", err)
	}
	if signatureA == "" {
		t.Fatalf("expected non-empty signature")
	}

	listenerPort = 20002
	signatureB, err := manager.runtimeSignature(task, []storage.Node{{Name: "node-a", ListenerPort: &listenerPort}})
	if err != nil {
		t.Fatalf("runtime signature B: %v", err)
	}
	if signatureA == signatureB {
		t.Fatalf("listener port change should change runtime signature")
	}

	if err := os.WriteFile(source, []byte(`proxies:
  - name: node-a
    type: ss
    server: changed.example.com
    port: 443
`), 0o600); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	signatureC, err := manager.runtimeSignature(task, []storage.Node{{Name: "node-a", ListenerPort: &listenerPort}})
	if err != nil {
		t.Fatalf("runtime signature C: %v", err)
	}
	if signatureB == signatureC {
		t.Fatalf("config content change should change runtime signature")
	}
}

func TestMihomoManagerDetectsExitedProcess(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "task.yaml")
	if err := os.WriteFile(source, []byte(`proxies:
  - name: node-a
    type: ss
    server: a.example.com
    port: 443
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	bin := filepath.Join(dir, "fake-mihomo.sh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 0.05\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake mihomo: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen controller: %v", err)
	}
	controller := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"version":"test"}`))
	})}
	go func() { _ = controller.Serve(listener) }()
	defer controller.Shutdown(context.Background())

	t.Setenv("MIHOMO_SECRET", "secret")
	settings := config.DefaultSettings()
	settings.Mihomo.Bin = bin
	settings.Mihomo.WorkDir = filepath.Join(dir, "mihomo")
	settings.Mihomo.ControllerHost = "127.0.0.1"
	settings.Mihomo.ControllerPort = listener.Addr().(*net.TCPAddr).Port

	manager := NewMihomoManager(settings)
	task := &storage.Task{ID: 1, ConfigPath: source}
	listenerPort := 20001
	if err := manager.Start(task, []storage.Node{{Name: "node-a", ListenerPort: &listenerPort}}); err != nil {
		t.Fatalf("start fake mihomo: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop() })

	deadline := time.Now().Add(2 * time.Second)
	for manager.isRunning() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if manager.isRunning() {
		t.Fatalf("expected exited mihomo process to be observed as stopped")
	}
	if manager.process != nil {
		t.Fatalf("expected exited mihomo process to be cleared, got %#v", manager.process)
	}
}
