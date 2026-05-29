package miaospeed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSidecarManagerStartsAndStopsExternalProcessAfterWebSocketReady(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		_ = conn.Close()
	}))
	defer server.Close()

	manager := NewSidecarManager(SidecarOptions{
		Enabled:        true,
		Bin:            "/bin/sh",
		Args:           []string{"-c", "sleep 30"},
		WorkDir:        t.TempDir(),
		WSURL:          "ws" + server.URL[len("http"):],
		StartTimeoutMS: 1000,
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	if !manager.Running() {
		t.Fatalf("expected sidecar process to be running")
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("stop sidecar: %v", err)
	}
	if manager.Running() {
		t.Fatalf("expected sidecar process to be stopped")
	}
}

func TestSidecarManagerDisabledIsNoop(t *testing.T) {
	manager := NewSidecarManager(SidecarOptions{Enabled: false, Bin: "/missing"})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("disabled sidecar should not start: %v", err)
	}
	if manager.Running() {
		t.Fatalf("disabled sidecar should not be running")
	}
}

func TestSidecarManagerBuildsDefaultServerCommandAndEnvFromTokenAndWSURL(t *testing.T) {
	manager := NewSidecarManager(SidecarOptions{
		Enabled: true,
		Bin:     "/bin/miaospeed",
		Token:   "server-token",
		WSURL:   "ws://127.0.0.1:8766/ws",
	})

	args, err := manager.commandArgs()
	if err != nil {
		t.Fatalf("command args: %v", err)
	}
	want := []string{"server"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("unexpected default args: %#v", args)
	}
	env, err := manager.commandEnv()
	if err != nil {
		t.Fatalf("command env: %v", err)
	}
	wantEnv := []string{"TOKEN=server-token", "BIND=127.0.0.1:8766"}
	if strings.Join(env, "\x00") != strings.Join(wantEnv, "\x00") {
		t.Fatalf("unexpected default env: %#v", env)
	}
}

func TestSidecarManagerDefaultServerArgsRequireToken(t *testing.T) {
	manager := NewSidecarManager(SidecarOptions{
		Enabled: true,
		Bin:     "/bin/miaospeed",
		WSURL:   "ws://127.0.0.1:8766",
	})

	_, err := manager.commandArgs()
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("expected token error, got %v", err)
	}
}

func TestSidecarManagerRequiresBinaryWhenEnabled(t *testing.T) {
	manager := NewSidecarManager(SidecarOptions{Enabled: true, WSURL: "ws://127.0.0.1:1"})
	err := manager.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("expected binary error, got %v", err)
	}
}
