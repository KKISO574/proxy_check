package main

import (
	"testing"

	"proxycheck/backend/internal/config"
)

func TestMiaoSpeedSidecarOptionsRequireGlobalEnablement(t *testing.T) {
	settings := config.DefaultSettings()
	settings.MiaoSpeed.ManageSidecar = true
	settings.MiaoSpeed.Enabled = false

	options := miaoSpeedSidecarOptions(settings, "server-token")
	if options.Enabled {
		t.Fatalf("sidecar should stay disabled when miaospeed.enabled is false")
	}

	settings.MiaoSpeed.Enabled = true
	options = miaoSpeedSidecarOptions(settings, "server-token")
	if !options.Enabled {
		t.Fatalf("sidecar should be enabled when miaospeed.enabled and manage_sidecar are both true")
	}
}

func TestEnvFirstPrefersNewAddressVariable(t *testing.T) {
	t.Setenv("PROXY_CHECK_ADDR", ":9000")
	t.Setenv("PROXY_CHECK_GO_ADDR", ":8001")

	if got := envFirst([]string{"PROXY_CHECK_ADDR", "PROXY_CHECK_GO_ADDR"}, ":8000"); got != ":9000" {
		t.Fatalf("envFirst should prefer PROXY_CHECK_ADDR, got %q", got)
	}
}

func TestEnvFirstKeepsLegacyAddressFallback(t *testing.T) {
	t.Setenv("PROXY_CHECK_GO_ADDR", ":8001")

	if got := envFirst([]string{"PROXY_CHECK_ADDR", "PROXY_CHECK_GO_ADDR"}, ":8000"); got != ":8001" {
		t.Fatalf("envFirst should keep legacy fallback, got %q", got)
	}
}
