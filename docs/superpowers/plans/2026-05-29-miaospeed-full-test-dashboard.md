# MiaoSpeed Full Test Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a MiaoSpeed-centered advanced test workflow that can run download/upload, DNS leak, network quality, and streaming unlock tests from the frontend, then display a spreadsheet-like result grid and exportable result image.

**Architecture:** Keep Mihomo as the baseline proxy runtime for regular polling, and use AirportR/MiaoSpeed only for heavy opt-in jobs. Store MiaoSpeed output as structured metric JSON in `probe_results`, expose a dedicated advanced-test API, and render a dense result matrix in the React dashboard with per-service columns, speed bars, status colors, charts, and PNG export.

**Tech Stack:** Go 1.23 backend, `net/http`, SQLite, AirportR/MiaoSpeed WebSocket protocol, React/Vite/TypeScript, Recharts, `html-to-image` for result export.

---

## Feature Scope From Reference Image

The reference image is a dense proxy-test result sheet. The first production version should cover these groups:

- Node identity: index, node name, proxy type, task name, status.
- Network quality: delay, HTTP ping, RTT stats, packet loss, HTTP status code, hijack detection, UDP/NAT type when available.
- Throughput: download average, download max, download per-second bar, upload average, upload max, upload per-second bar.
- Geo and carrier: inbound IP, outbound IP, country/region, ASN, ISP.
- DNS: DNS leak status and resolver list.
- Streaming and service unlock: Netflix, Disney+, YouTube, TikTok, OpenAI, Google, GitHub, Telegram, Spotify, Steam, Bilibili, Abema, DAZN, Hulu, Prime Video, HBO Max, Bahamut, BBC iPlayer, Claude, Gemini.
- Frontend control: task-level advanced-test enablement, manual run button, selectable test profile, service-column chooser, progress/status, result matrix, node detail charts, and export image button.

The service list is intentionally data-driven: services without a configured script render as `未配置`, services that fail render as `失败`, and services that return a region/status render the returned value.

---

## File Structure

- Create: `backend/internal/miaospeed/catalog.go`
  - Owns AirportR/MiaoSpeed matrix constants, result keys, default service catalog, and status normalization helpers.
- Modify: `backend/internal/miaospeed/adapter.go`
  - Adds upload matrix helpers and convenience accessors for numeric/string matrix payloads.
- Modify: `backend/internal/miaospeed/adapter_test.go`
  - Covers `ApiVersion=3`, upload fields, matrix payload normalization, and script-key mapping.
- Modify: `backend/internal/config/config.go`
  - Adds upload settings, full-test profile defaults, and unlock service script path configuration.
- Modify: `configs/config.example.yaml` and `configs/config.docker.yaml`
  - Documents the full MiaoSpeed profile, upload URL, service script paths, and safe default disabled state.
- Create: `backend/internal/probe/miaospeed_full.go`
  - Adds `MiaoSpeedFullProber`, which builds one AirportR request per node and records one structured `miaospeed_full` result.
- Create: `backend/internal/probe/miaospeed_full_test.go`
  - Covers request matrix selection, parsed result JSON, service script loading, missing script reporting, and upload/download extraction.
- Modify: `backend/internal/probe/factory.go`
  - Registers `miaospeed_full` only when `miaospeed.enabled` is true.
- Modify: `backend/internal/probe/service.go`
  - Adds a dedicated `RunAdvancedTask` path that runs only advanced probers and never runs during normal scheduler loops.
- Modify: `backend/internal/api/server.go`
  - Adds `POST /api/tasks/{id}/miaospeed/run`, `GET /api/tasks/{id}/miaospeed/results`, and `GET /api/miaospeed/catalog`.
- Modify: `backend/internal/storage/models.go`
  - Adds response structs for parsed latest `miaospeed_full` JSON per node.
- Modify: `frontend/src/api.ts`
  - Adds MiaoSpeed catalog/result/run types and API functions.
- Modify: `frontend/src/main.tsx`
  - Adds advanced test controls, result matrix view, profile/service filters, and node detail charts.
- Modify: `frontend/src/styles.css`
  - Adds dense table, status cell, speed bar, sticky-column, and export layout styling.
- Modify: `frontend/package.json` and `frontend/package-lock.json`
  - Adds `html-to-image` for PNG export.
- Modify: `README.md` and `AGENT.md`
  - Documents MiaoSpeed full-test workflow, safe defaults, and rollout state.

---

### Task 1: Add MiaoSpeed Catalog And Result Types

**Files:**
- Create: `backend/internal/miaospeed/catalog.go`
- Modify: `backend/internal/miaospeed/adapter.go`
- Modify: `backend/internal/miaospeed/adapter_test.go`

- [ ] **Step 1: Write catalog tests**

Add these tests to `backend/internal/miaospeed/adapter_test.go`:

```go
func TestDefaultFullTestCatalogIncludesScreenshotServices(t *testing.T) {
	catalog := DefaultServiceCatalog()
	keys := make(map[string]bool, len(catalog))
	for _, service := range catalog {
		keys[service.Key] = true
	}
	for _, key := range []string{
		"netflix", "disney", "youtube", "tiktok", "openai", "google",
		"github", "telegram", "spotify", "steam", "bilibili", "abema",
		"dazn", "hulu", "prime_video", "hbo_max", "bahamut", "bbc_iplayer",
		"claude", "gemini",
	} {
		if !keys[key] {
			t.Fatalf("missing service %s in catalog %#v", key, keys)
		}
	}
}

func TestMiaoSpeedMatrixPayloadHelpersReadUploadAndDownloadValues(t *testing.T) {
	node := NodeResult{Matrices: map[string]MatrixResult{
		MatrixSpeedAverage:  {Type: MatrixSpeedAverage, Payload: map[string]any{"Value": float64(1_250_000)}},
		MatrixUSpeedAverage: {Type: MatrixUSpeedAverage, Payload: map[string]any{"Value": float64(625_000)}},
		MatrixPacketLoss:    {Type: MatrixPacketLoss, Payload: map[string]any{"Value": float64(25)}},
		MatrixHTTPCode:      {Type: MatrixHTTPCode, Payload: map[string]any{"Value": "204"}},
	}}
	if got := node.AverageSpeedMbps(); got == nil || *got != 10 {
		t.Fatalf("download Mbps = %v", got)
	}
	if got := node.AverageUploadMbps(); got == nil || *got != 5 {
		t.Fatalf("upload Mbps = %v", got)
	}
	if got := node.MatrixNumber(MatrixPacketLoss, "Value"); got == nil || *got != 25 {
		t.Fatalf("packet loss = %v", got)
	}
	if got := node.MatrixString(MatrixHTTPCode, "Value"); got != "204" {
		t.Fatalf("http code = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/internal/miaospeed -run 'TestDefaultFullTestCatalogIncludesScreenshotServices|TestMiaoSpeedMatrixPayloadHelpersReadUploadAndDownloadValues' -count=1 -v
```

Expected: fail because `DefaultServiceCatalog`, upload helpers, and matrix constants do not exist yet.

- [ ] **Step 3: Add catalog and helpers**

Create `backend/internal/miaospeed/catalog.go`:

```go
package miaospeed

const (
	MatrixSpeedAverage       = "SPEED_AVERAGE"
	MatrixSpeedMax           = "SPEED_MAX"
	MatrixSpeedPerSecond     = "SPEED_PER_SECOND"
	MatrixUSpeedAverage      = "USPEED_AVERAGE"
	MatrixUSpeedMax          = "USPEED_MAX"
	MatrixUSpeedPerSecond    = "USPEED_PER_SECOND"
	MatrixHTTPPing           = "TEST_PING_CONN"
	MatrixRTTPing            = "TEST_PING_RTT"
	MatrixMaxRTTPing         = "TEST_PING_MAX_RTT"
	MatrixTotalHTTPPing      = "TEST_PING_TOTAL_CONN"
	MatrixTotalRTTPing       = "TEST_PING_TOTAL_RTT"
	MatrixSDRTT              = "TEST_PING_SD_RTT"
	MatrixSDHTTP             = "TEST_PING_SD_CONN"
	MatrixHTTPCode           = "TEST_HTTP_CODE"
	MatrixPacketLoss         = "TEST_PING_PACKET_LOSS"
	MatrixHijack             = "TEST_HIJACK_DETECTION"
	MatrixUDPType            = "UDP_TYPE"
	MatrixInboundGeoIP       = "GEOIP_INBOUND"
	MatrixOutboundGeoIP      = "GEOIP_OUTBOUND"
	MatrixScriptTest         = "TEST_SCRIPT"
)

type ServiceTestDefinition struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	ScriptID string `json:"script_id"`
	Group    string `json:"group"`
}

func DefaultServiceCatalog() []ServiceTestDefinition {
	return []ServiceTestDefinition{
		{Key: "netflix", Label: "Netflix", ScriptID: "netflix_unlock", Group: "streaming"},
		{Key: "disney", Label: "Disney+", ScriptID: "disney_unlock", Group: "streaming"},
		{Key: "youtube", Label: "YouTube", ScriptID: "youtube_unlock", Group: "streaming"},
		{Key: "tiktok", Label: "TikTok", ScriptID: "tiktok_unlock", Group: "streaming"},
		{Key: "openai", Label: "OpenAI", ScriptID: "openai_unlock", Group: "ai"},
		{Key: "google", Label: "Google", ScriptID: "google_unlock", Group: "service"},
		{Key: "github", Label: "GitHub", ScriptID: "github_unlock", Group: "service"},
		{Key: "telegram", Label: "Telegram", ScriptID: "telegram_unlock", Group: "service"},
		{Key: "spotify", Label: "Spotify", ScriptID: "spotify_unlock", Group: "streaming"},
		{Key: "steam", Label: "Steam", ScriptID: "steam_unlock", Group: "gaming"},
		{Key: "bilibili", Label: "Bilibili", ScriptID: "bilibili_unlock", Group: "streaming"},
		{Key: "abema", Label: "Abema", ScriptID: "abema_unlock", Group: "streaming"},
		{Key: "dazn", Label: "DAZN", ScriptID: "dazn_unlock", Group: "streaming"},
		{Key: "hulu", Label: "Hulu", ScriptID: "hulu_unlock", Group: "streaming"},
		{Key: "prime_video", Label: "Prime Video", ScriptID: "prime_video_unlock", Group: "streaming"},
		{Key: "hbo_max", Label: "HBO Max", ScriptID: "hbo_max_unlock", Group: "streaming"},
		{Key: "bahamut", Label: "Bahamut", ScriptID: "bahamut_unlock", Group: "streaming"},
		{Key: "bbc_iplayer", Label: "BBC iPlayer", ScriptID: "bbc_iplayer_unlock", Group: "streaming"},
		{Key: "claude", Label: "Claude", ScriptID: "claude_unlock", Group: "ai"},
		{Key: "gemini", Label: "Gemini", ScriptID: "gemini_unlock", Group: "ai"},
	}
}
```

Append these methods to `backend/internal/miaospeed/adapter.go` near the existing speed helpers:

```go
func (n NodeResult) AverageUploadMbps() *float64 {
	if value := matrixMbpsValue(n.Matrices[MatrixUSpeedAverage], "Value"); value != nil {
		return value
	}
	return matrixMbpsValue(n.Matrices[MatrixUSpeedPerSecond], "Average")
}

func (n NodeResult) MaxUploadMbps() *float64 {
	if value := matrixMbpsValue(n.Matrices[MatrixUSpeedMax], "Value"); value != nil {
		return value
	}
	return matrixMbpsValue(n.Matrices[MatrixUSpeedPerSecond], "Max")
}

func (n NodeResult) MatrixNumber(matrixName string, key string) *float64 {
	return matrixValue(n.Matrices[matrixName], key)
}

func (n NodeResult) MatrixString(matrixName string, keys ...string) string {
	matrix := n.Matrices[matrixName]
	switch payload := matrix.Payload.(type) {
	case string:
		return payload
	case map[string]any:
		for _, key := range keys {
			if value, ok := payload[key]; ok && value != nil {
				return fmt.Sprint(value)
			}
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test ./backend/internal/miaospeed -run 'TestDefaultFullTestCatalogIncludesScreenshotServices|TestMiaoSpeedMatrixPayloadHelpersReadUploadAndDownloadValues' -count=1 -v
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/miaospeed/catalog.go backend/internal/miaospeed/adapter.go backend/internal/miaospeed/adapter_test.go
git commit -m "feat: add miaospeed full test catalog"
```

### Task 2: Extend MiaoSpeed Configuration For Full Tests

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `configs/config.example.yaml`
- Modify: `configs/config.docker.yaml`

- [ ] **Step 1: Write failing config tests**

Add this test to `backend/internal/config/config_test.go`:

```go
func TestMiaoSpeedFullTestConfigLoadsUploadAndServiceScriptPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
miaospeed:
  enabled: true
  upload_url: https://speed.cloudflare.com/__up
  upload_duration_seconds: 4
  upload_threading: 2
  full_test_profile:
    include_download: true
    include_upload: true
    include_network_quality: true
    include_geo: true
    include_dns: true
    include_unlock: true
  service_script_paths:
    netflix: /app/runtime/miaospeed/scripts/netflix.js
    tiktok: /app/runtime/miaospeed/scripts/tiktok.js
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if settings.MiaoSpeed.UploadURL != "https://speed.cloudflare.com/__up" {
		t.Fatalf("unexpected upload url: %#v", settings.MiaoSpeed)
	}
	if !settings.MiaoSpeed.FullTestProfile.IncludeUnlock || !settings.MiaoSpeed.FullTestProfile.IncludeDownload {
		t.Fatalf("unexpected full profile: %#v", settings.MiaoSpeed.FullTestProfile)
	}
	if settings.MiaoSpeed.ServiceScriptPaths["tiktok"] != "/app/runtime/miaospeed/scripts/tiktok.js" {
		t.Fatalf("unexpected service paths: %#v", settings.MiaoSpeed.ServiceScriptPaths)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/internal/config -run TestMiaoSpeedFullTestConfigLoadsUploadAndServiceScriptPaths -count=1 -v
```

Expected: fail because the new fields are not defined.

- [ ] **Step 3: Extend config structs**

Modify `MiaoSpeedConfig` in `backend/internal/config/config.go`:

```go
type MiaoSpeedFullTestProfile struct {
	IncludeDownload       bool `yaml:"include_download"`
	IncludeUpload         bool `yaml:"include_upload"`
	IncludeNetworkQuality bool `yaml:"include_network_quality"`
	IncludeGeo            bool `yaml:"include_geo"`
	IncludeDNS            bool `yaml:"include_dns"`
	IncludeUnlock         bool `yaml:"include_unlock"`
}

type MiaoSpeedConfig struct {
	Enabled                 bool                    `yaml:"enabled"`
	ManageSidecar           bool                    `yaml:"manage_sidecar"`
	Bin                     string                  `yaml:"bin"`
	Args                    []string                `yaml:"args"`
	WorkDir                 string                  `yaml:"work_dir"`
	WSURL                   string                  `yaml:"ws_url"`
	TokenEnv                string                  `yaml:"token_env"`
	BuildTokenEnv           string                  `yaml:"build_token_env"`
	BuildTokens             []string                `yaml:"build_tokens"`
	Invoker                 string                  `yaml:"invoker"`
	TimeoutMS               int                     `yaml:"timeout_ms"`
	StartTimeoutMS          int                     `yaml:"start_timeout_ms"`
	DownloadURL             string                  `yaml:"download_url"`
	DownloadDurationSeconds int                     `yaml:"download_duration_seconds"`
	DownloadThreading       int                     `yaml:"download_threading"`
	UploadURL               string                  `yaml:"upload_url"`
	UploadDurationSeconds   int                     `yaml:"upload_duration_seconds"`
	UploadThreading         int                     `yaml:"upload_threading"`
	TaskTimeoutSeconds      int                     `yaml:"task_timeout_seconds"`
	MaxBandwidthConcurrency int                     `yaml:"max_bandwidth_concurrency"`
	ScriptTimeoutMS         int                     `yaml:"script_timeout_ms"`
	DNSLeakScript           string                  `yaml:"dns_leak_script"`
	DNSLeakScriptPath       string                  `yaml:"dns_leak_script_path"`
	UnlockScripts           map[string]string       `yaml:"unlock_scripts"`
	UnlockScriptPaths       map[string]string       `yaml:"unlock_script_paths"`
	ServiceScripts          map[string]string       `yaml:"service_scripts"`
	ServiceScriptPaths      map[string]string       `yaml:"service_script_paths"`
	FullTestProfile         MiaoSpeedFullTestProfile `yaml:"full_test_profile"`
}
```

Set defaults in `DefaultSettings()`:

```go
UploadURL:             "https://speed.cloudflare.com/__up",
UploadDurationSeconds: 5,
UploadThreading:       1,
ServiceScripts:        map[string]string{},
ServiceScriptPaths:    map[string]string{},
FullTestProfile: MiaoSpeedFullTestProfile{
	IncludeDownload:       true,
	IncludeUpload:         true,
	IncludeNetworkQuality: true,
	IncludeGeo:            true,
	IncludeDNS:            true,
	IncludeUnlock:         true,
},
```

- [ ] **Step 4: Document safe disabled defaults in YAML**

In both `configs/config.example.yaml` and `configs/config.docker.yaml`, add:

```yaml
  upload_url: https://speed.cloudflare.com/__up
  upload_duration_seconds: 5
  upload_threading: 1
  full_test_profile:
    include_download: true
    include_upload: true
    include_network_quality: true
    include_geo: true
    include_dns: true
    include_unlock: true
  service_scripts: {}
  service_script_paths:
    netflix: ""
    disney: ""
    youtube: ""
    tiktok: ""
    openai: ""
    google: ""
    github: ""
    telegram: ""
    spotify: ""
    steam: ""
    bilibili: ""
    abema: ""
    dazn: ""
    hulu: ""
    prime_video: ""
    hbo_max: ""
    bahamut: ""
    bbc_iplayer: ""
    claude: ""
    gemini: ""
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./backend/internal/config -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go configs/config.example.yaml configs/config.docker.yaml
git commit -m "feat: configure miaospeed full test profile"
```

### Task 3: Implement Full MiaoSpeed Prober

**Files:**
- Create: `backend/internal/probe/miaospeed_full.go`
- Create: `backend/internal/probe/miaospeed_full_test.go`
- Modify: `backend/internal/probe/factory.go`

- [ ] **Step 1: Write failing prober tests**

Create `backend/internal/probe/miaospeed_full_test.go`:

```go
package probe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"proxycheck/backend/internal/config"
	ms "proxycheck/backend/internal/miaospeed"
	"proxycheck/backend/internal/storage"
)

func TestMiaoSpeedFullProberBuildsFullRequestAndStoresStructuredData(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{frame: ms.Frame{
		ID:      "node-42-miaospeed-full",
		Version: "4.6.8",
		IsFinal: true,
		Nodes: []ms.NodeResult{{
			Name:             "node-a",
			InvokeDurationMS: 987,
			ProxyInfo:        map[string]any{"Name": "node-a", "Type": "Trojan"},
			Matrices: map[string]ms.MatrixResult{
				ms.MatrixSpeedAverage:  {Type: ms.MatrixSpeedAverage, Payload: map[string]any{"Value": float64(1_250_000)}},
				ms.MatrixUSpeedAverage: {Type: ms.MatrixUSpeedAverage, Payload: map[string]any{"Value": float64(625_000)}},
				ms.MatrixHTTPCode:      {Type: ms.MatrixHTTPCode, Payload: map[string]any{"Value": "204"}},
				ms.MatrixPacketLoss:    {Type: ms.MatrixPacketLoss, Payload: map[string]any{"Value": float64(0)}},
				"TEST_SCRIPT:netflix_unlock": {Type: ms.MatrixScriptTest, Payload: map[string]any{"Key": "netflix_unlock", "Text": "US"}},
			},
		}},
	}}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.DownloadURL = "https://example.com/down.bin"
	settings.UploadURL = "https://speed.cloudflare.com/__up"
	settings.ServiceScripts = map[string]string{"netflix": "function handler(){ return 'US'; }"}
	raw := "name: node-a\ntype: trojan\nserver: example.com\n"
	prober := MiaoSpeedFullProber{Settings: settings, Client: runner}

	results := prober.Probe(context.Background(), storage.Node{ID: 42, Name: "node-a", RawConfig: &raw})
	if len(results) != 1 || !results[0].Success || results[0].Metric != "miaospeed_full" {
		t.Fatalf("unexpected results: %#v", results)
	}
	if results[0].Data == nil || !strings.Contains(*results[0].Data, `"download_mbps":10`) {
		t.Fatalf("missing download result: %v", results[0].Data)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(*results[0].Data), &data); err != nil {
		t.Fatalf("decode result data: %v", err)
	}
	if data["upload_mbps"] != float64(5) {
		t.Fatalf("unexpected upload Mbps: %#v", data)
	}
	if data["version"] != "4.6.8" {
		t.Fatalf("unexpected version: %#v", data)
	}
	matrices := runner.request["Options"].(map[string]any)["Matrices"].([]map[string]string)
	if len(matrices) < 8 {
		t.Fatalf("expected full matrix request, got %#v", matrices)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/internal/probe -run TestMiaoSpeedFullProberBuildsFullRequestAndStoresStructuredData -count=1 -v
```

Expected: fail because `MiaoSpeedFullProber` does not exist.

- [ ] **Step 3: Implement prober**

Create `backend/internal/probe/miaospeed_full.go`:

```go
package probe

import (
	"context"
	"encoding/json"
	"fmt"

	"proxycheck/backend/internal/config"
	ms "proxycheck/backend/internal/miaospeed"
	"proxycheck/backend/internal/storage"
)

type MiaoSpeedFullProber struct {
	Settings config.MiaoSpeedConfig
	Client   MiaoSpeedRunner
}

func (p MiaoSpeedFullProber) AdvancedProbe() bool {
	return true
}

func (p MiaoSpeedFullProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	const metric = "miaospeed_full"
	if !p.Settings.Enabled {
		return nil
	}
	rawConfig, failure := requireMiaoSpeedRawConfig(metric, "full", node)
	if failure != nil {
		return []storage.ProbeResultInput{*failure}
	}
	if p.Client == nil {
		return []storage.ProbeResultInput{failedResult(metric, "full", "miaospeed client is not configured")}
	}
	requestConfig, matrices := p.buildRequestParts()
	frame, err := p.Client.Run(ctx, buildMiaoSpeedNodeRequest(
		p.Settings,
		node,
		rawConfig,
		"miaospeed-full",
		matrices,
		requestConfig,
	), nil)
	if err != nil {
		return []storage.ProbeResultInput{failedResult(metric, "full", err.Error())}
	}
	result := pickMiaoSpeedNode(frame, node.Name)
	if result == nil {
		return []storage.ProbeResultInput{failedResult(metric, "full", "MiaoSpeed response did not include node result")}
	}
	data, value := buildMiaoSpeedFullResult(frame, result)
	return []storage.ProbeResultInput{{
		Metric:  metric,
		Target:  "full",
		Value:   value,
		Data:    &data,
		Success: true,
	}}
}

func (p MiaoSpeedFullProber) buildRequestParts() (ms.RequestConfig, []ms.Matrix) {
	matrices := []ms.Matrix{}
	profile := p.Settings.FullTestProfile
	if profile.IncludeDownload {
		matrices = append(matrices,
			ms.Matrix{Type: ms.MatrixSpeedAverage},
			ms.Matrix{Type: ms.MatrixSpeedMax},
			ms.Matrix{Type: ms.MatrixSpeedPerSecond},
		)
	}
	if profile.IncludeUpload {
		matrices = append(matrices,
			ms.Matrix{Type: ms.MatrixUSpeedAverage},
			ms.Matrix{Type: ms.MatrixUSpeedMax},
			ms.Matrix{Type: ms.MatrixUSpeedPerSecond},
		)
	}
	if profile.IncludeNetworkQuality {
		matrices = append(matrices,
			ms.Matrix{Type: ms.MatrixHTTPPing},
			ms.Matrix{Type: ms.MatrixRTTPing},
			ms.Matrix{Type: ms.MatrixMaxRTTPing},
			ms.Matrix{Type: ms.MatrixTotalHTTPPing},
			ms.Matrix{Type: ms.MatrixTotalRTTPing},
			ms.Matrix{Type: ms.MatrixSDRTT},
			ms.Matrix{Type: ms.MatrixSDHTTP},
			ms.Matrix{Type: ms.MatrixHTTPCode},
			ms.Matrix{Type: ms.MatrixPacketLoss},
			ms.Matrix{Type: ms.MatrixHijack},
			ms.Matrix{Type: ms.MatrixUDPType},
		)
	}
	if profile.IncludeGeo {
		matrices = append(matrices, ms.Matrix{Type: ms.MatrixInboundGeoIP}, ms.Matrix{Type: ms.MatrixOutboundGeoIP})
	}
	scripts := []ms.Script{}
	if profile.IncludeDNS {
		if content, err := resolveMiaoSpeedScript(p.Settings.DNSLeakScript, p.Settings.DNSLeakScriptPath, "miaospeed.dns_leak_script_path"); err == nil && content != "" {
			matrices = append(matrices, ms.Matrix{Type: ms.MatrixScriptTest, Params: "dns_leak"})
			scripts = append(scripts, miaoSpeedScript(p.Settings, "dns_leak", content))
		}
	}
	if profile.IncludeUnlock {
		for _, service := range ms.DefaultServiceCatalog() {
			content := p.Settings.ServiceScripts[service.Key]
			path := p.Settings.ServiceScriptPaths[service.Key]
			if content == "" && path == "" {
				continue
			}
			resolved, err := resolveMiaoSpeedScript(content, path, "miaospeed.service_script_paths."+service.Key)
			if err != nil || resolved == "" {
				continue
			}
			matrices = append(matrices, ms.Matrix{Type: ms.MatrixScriptTest, Params: service.ScriptID})
			scripts = append(scripts, miaoSpeedScript(p.Settings, service.ScriptID, resolved))
		}
	}
	return ms.RequestConfig{
		ApiVersion:        3,
		DownloadURL:       p.Settings.DownloadURL,
		DownloadDuration:  p.Settings.DownloadDurationSeconds,
		DownloadThreading: p.Settings.DownloadThreading,
		UploadURL:         p.Settings.UploadURL,
		UploadDuration:    p.Settings.UploadDurationSeconds,
		UploadThreading:   p.Settings.UploadThreading,
		TaskTimeout:       p.Settings.TaskTimeoutSeconds,
		Scripts:           scripts,
	}, matrices
}

func buildMiaoSpeedFullResult(frame ms.Frame, result *ms.NodeResult) (string, *float64) {
	download := result.AverageSpeedMbps()
	payload := map[string]any{
		"version":            frame.Version,
		"invoke_duration_ms": result.InvokeDurationMS,
		"proxy_info":         result.ProxyInfo,
		"download_mbps":      download,
		"download_max_mbps":  result.MaxSpeedMbps(),
		"upload_mbps":        result.AverageUploadMbps(),
		"upload_max_mbps":    result.MaxUploadMbps(),
		"http_code":          result.MatrixString(ms.MatrixHTTPCode, "Value", "Code", "Status"),
		"packet_loss":        result.MatrixNumber(ms.MatrixPacketLoss, "Value"),
		"hijack":             result.MatrixString(ms.MatrixHijack, "Value", "Status", "Text"),
		"udp_type":           result.MatrixString(ms.MatrixUDPType, "Value", "Type", "Text"),
		"matrices":           result.Matrices,
		"services":           extractServiceStatuses(result),
	}
	raw, _ := json.Marshal(payload)
	return string(raw), download
}

func extractServiceStatuses(result *ms.NodeResult) map[string]string {
	statuses := map[string]string{}
	for _, service := range ms.DefaultServiceCatalog() {
		matrix := result.Matrices[fmt.Sprintf("%s:%s", ms.MatrixScriptTest, service.ScriptID)]
		status := matrixStatus(matrix, "Text", "Status", "Region", "Result", "Value")
		if status != "" {
			statuses[service.Key] = status
		}
	}
	return statuses
}
```

- [ ] **Step 4: Register `miaospeed_full`**

In `backend/internal/probe/factory.go`, add a case:

```go
case "miaospeed_full":
	if !settings.MiaoSpeed.Enabled {
		continue
	}
	probers = append(probers, MiaoSpeedFullProber{
		Settings: settings.MiaoSpeed,
		Client:   newMiaoSpeedClient(settings.MiaoSpeed),
	})
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./backend/internal/probe -run 'MiaoSpeed|BuildProbers' -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/probe/miaospeed_full.go backend/internal/probe/miaospeed_full_test.go backend/internal/probe/factory.go
git commit -m "feat: add miaospeed full test prober"
```

### Task 4: Add Advanced Run API And Result Query

**Files:**
- Modify: `backend/internal/probe/service.go`
- Modify: `backend/internal/probe/service_test.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/internal/api/server_test.go`
- Modify: `backend/internal/storage/models.go`
- Modify: `backend/internal/storage/repository_test.go`

- [ ] **Step 1: Write service test for advanced-only runs**

Add to `backend/internal/probe/service_test.go`:

```go
func TestRunAdvancedTaskRunsOnlyAdvancedProbers(t *testing.T) {
	dbPath := seedServiceSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()
	enabled := true
	if _, err := repo.UpdateTask(1, storage.TaskPatch{AdvancedProbesEnabled: &enabled}); err != nil {
		t.Fatalf("enable advanced probes: %v", err)
	}
	service := NewService(repo, Options{
		Probers: []Prober{
			StaticProber(func(storage.Node) []storage.ProbeResultInput {
				return []storage.ProbeResultInput{{Metric: "delay", Target: "delay-url", Success: true}}
			}),
			AdvancedStaticProber(func(storage.Node) []storage.ProbeResultInput {
				return []storage.ProbeResultInput{{Metric: "miaospeed_full", Target: "full", Success: true}}
			}),
		},
	})

	summary, err := service.RunAdvancedTask(1)
	if err != nil {
		t.Fatalf("run advanced: %v", err)
	}
	if summary.Nodes != 2 || summary.Results != 2 || summary.Errors != 0 {
		t.Fatalf("advanced run summary = %#v", summary)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open check db: %v", err)
	}
	defer db.Close()
	var normalCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM probe_results WHERE metric = 'delay'").Scan(&normalCount); err != nil {
		t.Fatalf("count normal results: %v", err)
	}
	if normalCount != 0 {
		t.Fatalf("RunAdvancedTask saved %d normal results", normalCount)
	}
	var advancedCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM probe_results WHERE metric = 'miaospeed_full'").Scan(&advancedCount); err != nil {
		t.Fatalf("count advanced results: %v", err)
	}
	if advancedCount != 2 {
		t.Fatalf("RunAdvancedTask saved %d advanced results", advancedCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/internal/probe -run TestRunAdvancedTaskRunsOnlyAdvancedProbers -count=1 -v
```

Expected: fail because `RunAdvancedTask` does not exist.

- [ ] **Step 3: Add service method**

Add to `backend/internal/probe/service.go`:

```go
func (s *Service) RunAdvancedTask(taskID int) (storage.RunSummary, error) {
	if !s.mu.TryLock() {
		return storage.RunSummary{Errors: 1}, nil
	}
	defer s.mu.Unlock()
	return s.runTaskWithPredicate(taskID, func(prober Prober) bool {
		return isAdvancedProber(prober)
	})
}
```

Refactor `runTaskUnlocked` to call:

```go
func (s *Service) runTaskUnlocked(taskID int) (storage.RunSummary, error) {
	return s.runTaskWithPredicate(taskID, func(Prober) bool { return true })
}
```

Inside `runNode`, add a filtered helper:

```go
func (s *Service) runNodeWithPredicate(ctx context.Context, task *storage.Task, node storage.Node, include func(Prober) bool) []storage.ProbeResultInput {
	results := make([]storage.ProbeResultInput, 0)
	for _, prober := range s.options.Probers {
		if !include(prober) {
			continue
		}
		if isAdvancedProber(prober) && (task == nil || !task.AdvancedProbesEnabled) {
			continue
		}
		results = append(results, prober.Probe(ctx, node)...)
	}
	return results
}
```

- [ ] **Step 4: Add API routes**

In `backend/internal/api/server.go`, extend `ProbeRunner`:

```go
type ProbeRunner interface {
	RunTask(taskID int) (RunSummary, error)
	RunAll() (RunSummary, error)
	RunAdvancedTask(taskID int) (RunSummary, error)
}
```

Add routes:

```go
s.mux.HandleFunc("POST /api/tasks/{id}/miaospeed/run", func(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "id")
	if !ok {
		return
	}
	if s.opts.Runner == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("probe runner is not configured"))
		return
	}
	summary, err := s.opts.Runner.RunAdvancedTask(id)
	writeJSON(w, summary, err)
})

s.mux.HandleFunc("GET /api/tasks/{id}/miaospeed/results", func(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "id")
	if !ok {
		return
	}
	nodes, err := repo.Nodes(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONValue(w, BuildMiaoSpeedResultGrid(nodes))
})

s.mux.HandleFunc("GET /api/miaospeed/catalog", func(w http.ResponseWriter, r *http.Request) {
	writeJSONValue(w, miaospeed.DefaultServiceCatalog())
})
```

Import `proxycheck/backend/internal/miaospeed` in `server.go`.

- [ ] **Step 5: Add result-grid builder**

Create response types in `backend/internal/storage/models.go`:

```go
type MiaoSpeedNodeResult struct {
	NodeID       int               `json:"node_id"`
	NodeName     string            `json:"node_name"`
	NodeType     *string           `json:"node_type"`
	Status       string            `json:"status"`
	DownloadMbps *float64          `json:"download_mbps"`
	UploadMbps   *float64          `json:"upload_mbps"`
	HTTPCode     string            `json:"http_code"`
	PacketLoss   *float64          `json:"packet_loss"`
	DNSLeak      *string           `json:"dns_leak"`
	Services     map[string]string `json:"services"`
	Raw          map[string]any    `json:"raw"`
	CreatedAt    *string           `json:"created_at"`
}

type MiaoSpeedResultGrid struct {
	Rows []MiaoSpeedNodeResult `json:"rows"`
}
```

Implement `BuildMiaoSpeedResultGrid` in `backend/internal/api/server.go` near Prometheus helpers:

```go
func BuildMiaoSpeedResultGrid(nodes []storage.Node) storage.MiaoSpeedResultGrid {
	rows := make([]storage.MiaoSpeedNodeResult, 0, len(nodes))
	for _, node := range nodes {
		metric, ok := node.Metrics["miaospeed_full"]
		if !ok || metric.Data == nil {
			rows = append(rows, storage.MiaoSpeedNodeResult{
				NodeID: node.ID, NodeName: node.Name, NodeType: node.Type, Status: "pending",
				Services: map[string]string{}, Raw: map[string]any{}, CreatedAt: nil,
			})
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(*metric.Data), &raw); err != nil {
			raw = map[string]any{"decode_error": err.Error()}
		}
		var dnsLeak *string
		if node.Meta != nil {
			dnsLeak = node.Meta.DNSLeak
		}
		rows = append(rows, storage.MiaoSpeedNodeResult{
			NodeID:       node.ID,
			NodeName:     node.Name,
			NodeType:     node.Type,
			Status:       node.Status,
			DownloadMbps: floatPtrFromAny(raw["download_mbps"]),
			UploadMbps:   floatPtrFromAny(raw["upload_mbps"]),
			HTTPCode:     stringFromAny(raw["http_code"]),
			PacketLoss:   floatPtrFromAny(raw["packet_loss"]),
			DNSLeak:      dnsLeak,
			Services:     stringMapFromAny(raw["services"]),
			Raw:          raw,
			CreatedAt:    &metric.CreatedAt,
		})
	}
	return storage.MiaoSpeedResultGrid{Rows: rows}
}

func floatPtrFromAny(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		return &typed
	case int:
		converted := float64(typed)
		return &converted
	default:
		return nil
	}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func stringMapFromAny(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		out[key] = fmt.Sprint(item)
	}
	return out
}
```

- [ ] **Step 6: Run backend tests**

Run:

```bash
go test ./backend/internal/probe ./backend/internal/api ./backend/internal/storage -count=1
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/probe/service.go backend/internal/probe/service_test.go backend/internal/api/server.go backend/internal/api/server_test.go backend/internal/storage/models.go backend/internal/storage/repository.go backend/internal/storage/repository_test.go
git commit -m "feat: expose miaospeed advanced run api"
```

### Task 5: Add Frontend Controls And API Types

**Files:**
- Modify: `frontend/src/api.ts`
- Modify: `frontend/src/main.tsx`
- Modify: `frontend/src/styles.css`

- [ ] **Step 1: Add frontend API types**

Add to `frontend/src/api.ts`:

```ts
export interface MiaoSpeedServiceDefinition {
  key: string;
  label: string;
  script_id: string;
  group: string;
}

export interface MiaoSpeedNodeResult {
  node_id: number;
  node_name: string;
  node_type: string | null;
  status: string;
  download_mbps: number | null;
  upload_mbps: number | null;
  http_code: string;
  packet_loss: number | null;
  dns_leak: string | null;
  services: Record<string, string>;
  raw: Record<string, unknown>;
  created_at: string | null;
}

export interface MiaoSpeedResultGrid {
  rows: MiaoSpeedNodeResult[];
}

export function fetchMiaoSpeedCatalog(): Promise<MiaoSpeedServiceDefinition[]> {
  return request<MiaoSpeedServiceDefinition[]>("/api/miaospeed/catalog");
}

export function runMiaoSpeedTask(id: number): Promise<{ nodes: number; results: number; errors: number }> {
  return request(`/api/tasks/${id}/miaospeed/run`, { method: "POST" });
}

export function fetchMiaoSpeedResults(taskId: number): Promise<MiaoSpeedResultGrid> {
  return request<MiaoSpeedResultGrid>(`/api/tasks/${taskId}/miaospeed/results`);
}
```

- [ ] **Step 2: Add advanced run button and state**

In `frontend/src/main.tsx`, import the new API functions and add state:

```ts
const [miaospeedCatalog, setMiaoSpeedCatalog] = useState<MiaoSpeedServiceDefinition[]>([]);
const [miaospeedGrid, setMiaoSpeedGrid] = useState<MiaoSpeedResultGrid>({ rows: [] });
const [miaospeedRunning, setMiaoSpeedRunning] = useState(false);
const [visibleServices, setVisibleServices] = useState<Record<string, boolean>>({});
```

Load catalog once:

```ts
useEffect(() => {
  fetchMiaoSpeedCatalog()
    .then((catalog) => {
      setMiaoSpeedCatalog(catalog);
      setVisibleServices(Object.fromEntries(catalog.map((service) => [service.key, true])));
    })
    .catch((error) => setError(error.message));
}, []);
```

Add a handler:

```ts
async function handleRunMiaoSpeedTask() {
  if (!selectedTaskId) return;
  setMiaoSpeedRunning(true);
  setError("");
  try {
    await runMiaoSpeedTask(selectedTaskId);
    const grid = await fetchMiaoSpeedResults(selectedTaskId);
    setMiaoSpeedGrid(grid);
    await loadTaskData(selectedTaskId);
  } catch (error) {
    setError(error instanceof Error ? error.message : String(error));
  } finally {
    setMiaoSpeedRunning(false);
  }
}
```

- [ ] **Step 3: Add matrix component**

Add a `MiaoSpeedMatrixPanel` component in `frontend/src/main.tsx`:

```tsx
function MiaoSpeedMatrixPanel({
  rows,
  catalog,
  visibleServices,
  onToggleService,
  onRun,
  running
}: {
  rows: MiaoSpeedNodeResult[];
  catalog: MiaoSpeedServiceDefinition[];
  visibleServices: Record<string, boolean>;
  onToggleService: (key: string) => void;
  onRun: () => void;
  running: boolean;
}) {
  const shown = catalog.filter((service) => visibleServices[service.key]);
  return (
    <section className="miaospeed-matrix-panel" id="miaospeed-result-export">
      <div className="panel-title">
        <h3>MiaoSpeed 全量测试</h3>
        <button className="primary-button" type="button" onClick={onRun} disabled={running}>
          <Activity size={16} className={running ? "spin" : ""} />
          {running ? "测试中" : "运行高级测试"}
        </button>
      </div>
      <div className="service-filter">
        {catalog.map((service) => (
          <label key={service.key}>
            <input
              type="checkbox"
              checked={visibleServices[service.key] ?? true}
              onChange={() => onToggleService(service.key)}
            />
            <span>{service.label}</span>
          </label>
        ))}
      </div>
      <div className="miaospeed-grid-wrap">
        <table className="miaospeed-grid">
          <thead>
            <tr>
              <th className="sticky-col">节点</th>
              <th>类型</th>
              <th>下载</th>
              <th>上传</th>
              <th>HTTP</th>
              <th>丢包</th>
              <th>DNS</th>
              {shown.map((service) => <th key={service.key}>{service.label}</th>)}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.node_id}>
                <td className="sticky-col">{row.node_name}</td>
                <td>{row.node_type ?? "-"}</td>
                <td><SpeedCell value={row.download_mbps} /></td>
                <td><SpeedCell value={row.upload_mbps} /></td>
                <td>{row.http_code || "-"}</td>
                <td>{row.packet_loss === null ? "-" : `${row.packet_loss}%`}</td>
                <td><StatusCell value={row.dns_leak} /></td>
                {shown.map((service) => (
                  <td key={service.key}><StatusCell value={row.services[service.key]} /></td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
```

- [ ] **Step 4: Add cells**

Add helper components in `frontend/src/main.tsx`:

```tsx
function SpeedCell({ value }: { value: number | null }) {
  const width = value === null ? 0 : Math.min(100, Math.round(value * 2));
  return (
    <div className="speed-cell">
      <span>{value === null ? "-" : `${value.toFixed(1)} Mbps`}</span>
      <i style={{ width: `${width}%` }} />
    </div>
  );
}

function StatusCell({ value }: { value?: string | null }) {
  const normalized = (value ?? "").toLowerCase();
  const className =
    normalized === "" ? "status-empty" :
    normalized.includes("fail") || normalized.includes("block") || normalized.includes("失败") || normalized.includes("否")
      ? "status-bad"
      : normalized.includes("partial") || normalized.includes("部分")
        ? "status-warn"
        : "status-good";
  return <span className={`matrix-status ${className}`}>{value || "-"}</span>;
}
```

- [ ] **Step 5: Style the matrix**

Add to `frontend/src/styles.css`:

```css
.miaospeed-matrix-panel {
  margin-top: 16px;
  border-top: 1px solid #d8e2ec;
  padding-top: 16px;
}
.service-filter {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 12px;
  margin-bottom: 12px;
  font-size: 12px;
}
.miaospeed-grid-wrap {
  max-height: 520px;
  overflow: auto;
  border: 1px solid #d8e2ec;
  border-radius: 8px;
  background: #fff;
}
.miaospeed-grid {
  min-width: 1600px;
  border-collapse: collapse;
  font-size: 12px;
}
.miaospeed-grid th,
.miaospeed-grid td {
  border-bottom: 1px solid #e5edf5;
  border-right: 1px solid #e5edf5;
  padding: 5px 8px;
  white-space: nowrap;
}
.miaospeed-grid th {
  position: sticky;
  top: 0;
  z-index: 2;
  background: #eef6ff;
}
.sticky-col {
  position: sticky;
  left: 0;
  z-index: 3;
  background: #fff;
}
.speed-cell {
  position: relative;
  min-width: 96px;
}
.speed-cell i {
  display: block;
  height: 4px;
  margin-top: 3px;
  background: #1d4ed8;
}
.matrix-status {
  display: inline-flex;
  min-width: 48px;
  justify-content: center;
  border-radius: 4px;
  padding: 2px 5px;
}
.status-good { background: #dcfce7; color: #166534; }
.status-warn { background: #fef3c7; color: #92400e; }
.status-bad { background: #fee2e2; color: #991b1b; }
.status-empty { background: #f1f5f9; color: #64748b; }
```

- [ ] **Step 6: Run frontend build**

Run:

```bash
npm --prefix frontend run build
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api.ts frontend/src/main.tsx frontend/src/styles.css
git commit -m "feat: add miaospeed result matrix controls"
```

### Task 6: Add Result Image Export

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`
- Modify: `frontend/src/main.tsx`
- Modify: `frontend/src/styles.css`

- [ ] **Step 1: Install export dependency**

Run:

```bash
npm --prefix frontend install html-to-image
```

Expected: `frontend/package.json` and `frontend/package-lock.json` include `html-to-image`.

- [ ] **Step 2: Add export button**

In `frontend/src/main.tsx`, add:

```ts
import { toPng } from "html-to-image";
```

Add handler:

```ts
async function handleExportMiaoSpeedImage() {
  const element = document.getElementById("miaospeed-result-export");
  if (!element) return;
  const dataUrl = await toPng(element, {
    backgroundColor: "#ffffff",
    pixelRatio: 2,
    cacheBust: true
  });
  const link = document.createElement("a");
  link.download = `miaospeed-results-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.png`;
  link.href = dataUrl;
  link.click();
}
```

Add a button next to the run button:

```tsx
<button className="ghost-button" type="button" onClick={handleExportMiaoSpeedImage}>
  <Download size={16} />
  导出结果图
</button>
```

- [ ] **Step 3: Ensure export uses stable width**

Add CSS:

```css
#miaospeed-result-export {
  background: #ffffff;
}
```

- [ ] **Step 4: Run build**

Run:

```bash
npm --prefix frontend run build
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/main.tsx frontend/src/styles.css
git commit -m "feat: export miaospeed result image"
```

### Task 7: Add AirportR 4.6.8 Integration Verification

**Files:**
- Modify: `backend/internal/miaospeed/integration_test.go`
- Modify: `README.md`

- [ ] **Step 1: Extend integration test with full matrix**

In `backend/internal/miaospeed/integration_test.go`, add a second request after the existing speed request:

```go
fullFrame, err := client.Run(context.Background(), BuildRequest(Request{
	TaskID:  "proxy-check-full-integration",
	Invoker: "proxy-check",
	Vendor:  "Clash",
	Nodes: []Node{
		{Name: "local-http", Payload: clashPayload},
	},
	Matrices: []Matrix{
		{Type: MatrixSpeedAverage},
		{Type: MatrixUSpeedAverage},
		{Type: MatrixHTTPPing},
		{Type: MatrixRTTPing},
		{Type: MatrixHTTPCode},
		{Type: MatrixPacketLoss},
		{Type: MatrixScriptTest, Params: "service_test"},
	},
	Config: RequestConfig{
		ApiVersion:        3,
		DownloadURL:       downloadTarget.URL,
		DownloadDuration:  1,
		DownloadThreading: 1,
		UploadURL:         "https://speed.cloudflare.com/__up",
		UploadDuration:    1,
		UploadThreading:   1,
		TaskTimeout:       10,
		Scripts: []Script{{
			ID:            "service_test",
			Type:          "media",
			Content:       `function handler(){ return "ok"; }`,
			TimeoutMillis: 1000,
		}},
	},
}), nil)
if err != nil {
	t.Fatalf("run miaospeed full request: %v", err)
}
if len(fullFrame.Nodes) == 0 {
	t.Fatalf("expected full result node, got %#v", fullFrame)
}
```

- [ ] **Step 2: Run opt-in integration test**

Run with a real AirportR 4.6.8 binary:

```bash
PROXY_CHECK_MIAOSPEED_INTEGRATION=1 \
MIAOSPEED_BIN=/absolute/path/to/miaospeed \
MIAOSPEED_TOKEN=your_token \
go test ./backend/internal/miaospeed -run TestMiaoSpeedSidecarIntegration -count=1 -v
```

Expected: pass against AirportR 4.6.8. If `cannot verify the request` appears, set `MIAOSPEED_BUILD_TOKENS` to the token segments used by that binary and rerun the same command.

- [ ] **Step 3: Document verification**

Update `README.md` under `MiaoSpeed 集成状态` with:

```markdown
全量测试模式会使用 AirportR 4.6.x 的 `ApiVersion=3` 请求，覆盖下载、上传、
HTTP/RTT 质量、丢包、HTTP code、DNS/服务脚本和 Geo/UDP 类矩阵。该模式只通过
前端“运行高级测试”手动触发，不参与普通定时轮询。
```

- [ ] **Step 4: Commit**

```bash
git add backend/internal/miaospeed/integration_test.go README.md
git commit -m "test: verify airportr miaospeed full matrix"
```

### Task 8: Final Verification And Documentation

**Files:**
- Modify: `AGENT.md`
- Modify: `README.md`

- [ ] **Step 1: Update roadmap summary**

In `AGENT.md`, add a new `P1 MiaoSpeed 全量测试与结果图` section if it is not already present:

```markdown
### P1 MiaoSpeed 全量测试与结果图

- 目标对齐 MiaoSpeed 结果图：节点、延迟、下载/上传、DNS、Geo、丢包、HTTP code、
  劫持检测、UDP/NAT、服务解锁状态按列展示。
- 后端新增 `miaospeed_full` 高级探测维度和手动运行 API，普通调度不自动触发。
- 前端新增“运行高级测试”、服务列选择、结果矩阵、速度条、状态色块、节点详情图表和 PNG 导出。
- 服务解锁使用 `TEST_SCRIPT`，脚本从 `runtime/miaospeed/scripts/` 加载；未配置脚本的服务显示 `未配置`。
```

- [ ] **Step 2: Run full automated checks**

Run:

```bash
go test ./...
npm --prefix frontend run build
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 3: Manual browser check**

Start the app locally:

```bash
export MIHOMO_SECRET=dev_secret
export MIAOSPEED_TOKEN=dev_token
PROXY_CHECK_CONFIG=configs/config.yaml go run ./backend/cmd/proxy-check
```

Open `http://127.0.0.1:8000/`, verify:

- Task form still creates/updates tasks.
- Advanced test button is visible only inside the operational dashboard, not on a landing page.
- Result grid scrolls horizontally with the node column sticky.
- Status colors match success/warn/failure/empty.
- Export button downloads a PNG containing the matrix.

- [ ] **Step 4: Commit**

```bash
git add AGENT.md README.md
git commit -m "docs: document miaospeed full test dashboard"
```

---

## Self-Review

- Spec coverage: backend MiaoSpeed matrices, frontend controls, result grid, export image, service unlock scripts, and safe opt-in execution are covered by Tasks 1 through 8.
- Placeholder scan: the plan defines concrete files, commands, route names, metric names, and code snippets for every implementation task.
- Type consistency: the plan uses `miaospeed_full`, `MiaoSpeedFullProber`, `MiaoSpeedResultGrid`, `MiaoSpeedNodeResult`, `RunAdvancedTask`, and `DefaultServiceCatalog` consistently across backend and frontend tasks.
