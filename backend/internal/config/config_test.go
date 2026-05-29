package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsMergesConfigOverDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
app:
  database_url: sqlite:///./custom.sqlite3
mihomo:
  bin: /usr/local/bin/mihomo
  controller_port: 19090
probe:
  concurrency: 7
  dimensions:
    - delay
    - tcping
  tcp_targets:
    - host: 9.9.9.9
      port: 443
miaospeed:
  enabled: true
  manage_sidecar: true
  bin: /usr/local/bin/miaospeed
  args:
    - server
    - -bind
    - 127.0.0.1:8766
  work_dir: /tmp/miaospeed
  token_env: TEST_MIAOSPEED_TOKEN
  build_token_env: TEST_MIAOSPEED_BUILD_TOKENS
  build_tokens:
    - build-a
    - build-b
  dns_leak_script_path: /etc/proxy-check/scripts/dns-leak.js
  unlock_script_paths:
    netflix: /etc/proxy-check/scripts/netflix.js
    openai: /etc/proxy-check/scripts/openai.js
  start_timeout_ms: 2500
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	settings, err := LoadSettings(configPath)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if settings.App.DatabaseURL != "sqlite:///./custom.sqlite3" {
		t.Fatalf("unexpected database url: %s", settings.App.DatabaseURL)
	}
	if settings.Mihomo.Bin != "/usr/local/bin/mihomo" || settings.Mihomo.ControllerPort != 19090 {
		t.Fatalf("unexpected mihomo config: %#v", settings.Mihomo)
	}
	if settings.Probe.Concurrency != 7 || len(settings.Probe.Dimensions) != 2 {
		t.Fatalf("unexpected probe config: %#v", settings.Probe)
	}
	if settings.Probe.TCPTargets[0].Host != "9.9.9.9" || settings.Probe.TCPTargets[0].Port != 443 {
		t.Fatalf("unexpected tcp targets: %#v", settings.Probe.TCPTargets)
	}
	if settings.Mihomo.ListenerPortStart != 20000 {
		t.Fatalf("expected default listener port start, got %d", settings.Mihomo.ListenerPortStart)
	}
	if !settings.MiaoSpeed.Enabled || !settings.MiaoSpeed.ManageSidecar {
		t.Fatalf("unexpected miaospeed enablement: %#v", settings.MiaoSpeed)
	}
	if settings.MiaoSpeed.Bin != "/usr/local/bin/miaospeed" || settings.MiaoSpeed.WorkDir != "/tmp/miaospeed" {
		t.Fatalf("unexpected miaospeed paths: %#v", settings.MiaoSpeed)
	}
	if settings.MiaoSpeed.TokenEnv != "TEST_MIAOSPEED_TOKEN" || settings.MiaoSpeed.BuildTokenEnv != "TEST_MIAOSPEED_BUILD_TOKENS" {
		t.Fatalf("unexpected miaospeed token env config: %#v", settings.MiaoSpeed)
	}
	if len(settings.MiaoSpeed.BuildTokens) != 2 || settings.MiaoSpeed.BuildTokens[0] != "build-a" || settings.MiaoSpeed.BuildTokens[1] != "build-b" {
		t.Fatalf("unexpected miaospeed build tokens: %#v", settings.MiaoSpeed.BuildTokens)
	}
	if len(settings.MiaoSpeed.Args) != 3 || settings.MiaoSpeed.Args[0] != "server" || settings.MiaoSpeed.StartTimeoutMS != 2500 {
		t.Fatalf("unexpected miaospeed args/timeout: %#v", settings.MiaoSpeed)
	}
	if settings.MiaoSpeed.DNSLeakScriptPath != "/etc/proxy-check/scripts/dns-leak.js" {
		t.Fatalf("unexpected dns leak script path: %q", settings.MiaoSpeed.DNSLeakScriptPath)
	}
	if len(settings.MiaoSpeed.UnlockScriptPaths) != 2 || settings.MiaoSpeed.UnlockScriptPaths["netflix"] != "/etc/proxy-check/scripts/netflix.js" {
		t.Fatalf("unexpected unlock script paths: %#v", settings.MiaoSpeed.UnlockScriptPaths)
	}
}

func TestSQLitePathExtractsFilePathFromSQLAlchemyStyleURL(t *testing.T) {
	cases := map[string]string{
		"sqlite+aiosqlite:///./data/proxy_check.sqlite3": "./data/proxy_check.sqlite3",
		"sqlite:///./data/proxy_check.sqlite3":           "./data/proxy_check.sqlite3",
		"/tmp/proxy_check.sqlite3":                       "/tmp/proxy_check.sqlite3",
	}
	for input, want := range cases {
		if got := SQLitePath(input); got != want {
			t.Fatalf("SQLitePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDefaultSettingsUseAirportRMiaoSpeedBuildToken(t *testing.T) {
	settings := DefaultSettings()
	if len(settings.MiaoSpeed.BuildTokens) != len(AirportRMiaoSpeedBuildTokens) {
		t.Fatalf("unexpected default build token count: %#v", settings.MiaoSpeed.BuildTokens)
	}
	if settings.MiaoSpeed.BuildTokens[0] != "MIAOKO4" || settings.MiaoSpeed.BuildTokens[len(settings.MiaoSpeed.BuildTokens)-1] != "T0kEN" {
		t.Fatalf("unexpected AirportR MiaoSpeed build token defaults: %#v", settings.MiaoSpeed.BuildTokens)
	}
}

func TestBundledConfigFilesLoad(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, path := range []string{
		"configs/config.example.yaml",
		"configs/config.docker.yaml",
	} {
		settings, err := LoadSettings(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		if settings.App.DatabaseURL == "" || settings.App.StaticDir == "" {
			t.Fatalf("%s did not load app settings: %#v", path, settings.App)
		}
		if settings.Mihomo.Bin == "" || settings.MiaoSpeed.WSURL == "" {
			t.Fatalf("%s did not load runtime binary settings: mihomo=%q miaospeed_ws=%q", path, settings.Mihomo.Bin, settings.MiaoSpeed.WSURL)
		}
	}

	dockerSettings, err := LoadSettings(filepath.Join(root, "configs/config.docker.yaml"))
	if err != nil {
		t.Fatalf("load docker config: %v", err)
	}
	if dockerSettings.MiaoSpeed.WSURL != "ws://127.0.0.1:8766" {
		t.Fatalf("docker sidecar ws_url should use container-local address, got %q", dockerSettings.MiaoSpeed.WSURL)
	}
}
