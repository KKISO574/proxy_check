package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Settings struct {
	App       AppConfig       `yaml:"app"`
	Mihomo    MihomoConfig    `yaml:"mihomo"`
	Probe     ProbeConfig     `yaml:"probe"`
	MiaoSpeed MiaoSpeedConfig `yaml:"miaospeed"`
}

type AppConfig struct {
	DatabaseURL string `yaml:"database_url"`
	StaticDir   string `yaml:"static_dir"`
}

type MihomoConfig struct {
	Bin               string `yaml:"bin"`
	SourceConfigPath  string `yaml:"source_config_path"`
	WorkDir           string `yaml:"work_dir"`
	ImportedConfigDir string `yaml:"imported_config_dir"`
	ControllerHost    string `yaml:"controller_host"`
	ControllerPort    int    `yaml:"controller_port"`
	SecretEnv         string `yaml:"secret_env"`
	ListenerHost      string `yaml:"listener_host"`
	ListenerPortStart int    `yaml:"listener_port_start"`
	ListenerPortMax   int    `yaml:"listener_port_max"`
}

type ProbeConfig struct {
	IntervalSeconds int         `yaml:"interval_seconds"`
	Concurrency     int         `yaml:"concurrency"`
	TimeoutMS       int         `yaml:"timeout_ms"`
	ImportTimeoutMS int         `yaml:"import_timeout_ms"`
	RetentionDays   int         `yaml:"retention_days"`
	DelayURL        string      `yaml:"delay_url"`
	Dimensions      []string    `yaml:"dimensions"`
	TCPTargets      []TCPTarget `yaml:"tcp_targets"`
}

type TCPTarget struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type MiaoSpeedConfig struct {
	Enabled                 bool              `yaml:"enabled"`
	ManageSidecar           bool              `yaml:"manage_sidecar"`
	Bin                     string            `yaml:"bin"`
	Args                    []string          `yaml:"args"`
	WorkDir                 string            `yaml:"work_dir"`
	WSURL                   string            `yaml:"ws_url"`
	TokenEnv                string            `yaml:"token_env"`
	BuildTokenEnv           string            `yaml:"build_token_env"`
	BuildTokens             []string          `yaml:"build_tokens"`
	Invoker                 string            `yaml:"invoker"`
	TimeoutMS               int               `yaml:"timeout_ms"`
	StartTimeoutMS          int               `yaml:"start_timeout_ms"`
	DownloadURL             string            `yaml:"download_url"`
	DownloadDurationSeconds int               `yaml:"download_duration_seconds"`
	DownloadThreading       int               `yaml:"download_threading"`
	TaskTimeoutSeconds      int               `yaml:"task_timeout_seconds"`
	MaxBandwidthConcurrency int               `yaml:"max_bandwidth_concurrency"`
	ScriptTimeoutMS         int               `yaml:"script_timeout_ms"`
	DNSLeakScript           string            `yaml:"dns_leak_script"`
	DNSLeakScriptPath       string            `yaml:"dns_leak_script_path"`
	UnlockScripts           map[string]string `yaml:"unlock_scripts"`
	UnlockScriptPaths       map[string]string `yaml:"unlock_script_paths"`
}

func DefaultSettings() Settings {
	return Settings{
		App: AppConfig{
			DatabaseURL: "sqlite:///./data/proxy_check.sqlite3",
			StaticDir:   "web/static",
		},
		Mihomo: MihomoConfig{
			Bin:               "./runtime/bin/mihomo",
			WorkDir:           "./runtime/mihomo",
			ImportedConfigDir: "./runtime/configs",
			ControllerHost:    "127.0.0.1",
			ControllerPort:    9090,
			SecretEnv:         "MIHOMO_SECRET",
			ListenerHost:      "127.0.0.1",
			ListenerPortStart: 20000,
			ListenerPortMax:   65000,
		},
		Probe: ProbeConfig{
			IntervalSeconds: 60,
			Concurrency:     100,
			TimeoutMS:       5000,
			ImportTimeoutMS: 30000,
			RetentionDays:   30,
			DelayURL:        "https://cp.cloudflare.com/generate_204",
			Dimensions:      []string{"delay", "tcping", "tls_handshake", "http_rtt", "jitter", "packet_loss", "exit_geo"},
			TCPTargets: []TCPTarget{
				{Host: "1.1.1.1", Port: 443},
				{Host: "1.1.1.1", Port: 80},
				{Host: "8.8.8.8", Port: 443},
				{Host: "8.8.8.8", Port: 80},
			},
		},
		MiaoSpeed: MiaoSpeedConfig{
			WorkDir:                 "./runtime/miaospeed",
			WSURL:                   "ws://127.0.0.1:8766",
			TokenEnv:                "MIAOSPEED_TOKEN",
			BuildTokenEnv:           "MIAOSPEED_BUILD_TOKENS",
			Invoker:                 "proxy-check",
			TimeoutMS:               30000,
			StartTimeoutMS:          10000,
			DownloadDurationSeconds: 5,
			DownloadThreading:       2,
			TaskTimeoutSeconds:      30,
			MaxBandwidthConcurrency: 1,
			ScriptTimeoutMS:         10000,
			UnlockScripts:           map[string]string{},
			UnlockScriptPaths:       map[string]string{},
		},
	}
}

func LoadSettings(path string) (Settings, error) {
	settings := DefaultSettings()
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return Settings{}, err
	}
	if err := yaml.Unmarshal(content, &settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func SQLitePath(databaseURL string) string {
	for _, prefix := range []string{"sqlite+aiosqlite:///", "sqlite:///"} {
		if strings.HasPrefix(databaseURL, prefix) {
			return strings.TrimPrefix(databaseURL, prefix)
		}
	}
	return databaseURL
}
