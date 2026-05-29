package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxycheck/backend/internal/config"
	ms "proxycheck/backend/internal/miaospeed"
	"proxycheck/backend/internal/storage"
)

func TestMiaoSpeedBandwidthProberBuildsRequestAndRecordsAverageSpeed(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{
		frame: ms.Frame{
			ID:      "node-7-miaospeed-bandwidth",
			Version: "test-version",
			IsFinal: true,
			Nodes: []ms.NodeResult{
				{
					Name:             "node-a",
					InvokeDurationMS: 1234,
					Matrices: map[string]ms.MatrixResult{
						"SPEED_AVERAGE": {
							Type:    "SPEED_AVERAGE",
							Payload: map[string]any{"Value": float64(1_562_500)},
						},
						"SPEED_MAX": {
							Type:    "SPEED_MAX",
							Payload: map[string]any{"Value": float64(2_500_000)},
						},
					},
				},
			},
		},
	}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.DownloadURL = "https://example.com/10m.bin"
	settings.Invoker = "proxy-check-test"
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	prober := MiaoSpeedBandwidthProber{Settings: settings, Client: runner}

	results := prober.Probe(context.Background(), storage.Node{ID: 7, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 {
		t.Fatalf("expected one bandwidth result, got %#v", results)
	}
	result := results[0]
	if result.Metric != "miaospeed_bandwidth" || result.Target != settings.DownloadURL || !result.Success {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Value == nil || *result.Value != 12.5 {
		t.Fatalf("unexpected average speed: %#v", result.Value)
	}
	if result.Data == nil || !strings.Contains(*result.Data, `"max_mbps":20`) {
		t.Fatalf("expected max speed in data, got %v", result.Data)
	}
	if runner.request["Vendor"] != "Clash" {
		t.Fatalf("expected Clash vendor request, got %#v", runner.request["Vendor"])
	}
	nodes := runner.request["Nodes"].([]map[string]string)
	if len(nodes) != 1 || nodes[0]["Payload"] != rawConfig {
		t.Fatalf("unexpected node payload: %#v", nodes)
	}
}

func TestMiaoSpeedBandwidthProberRequiresExplicitEnablementAndRawConfig(t *testing.T) {
	settings := config.DefaultSettings().MiaoSpeed
	settings.DownloadURL = "https://example.com/10m.bin"
	prober := MiaoSpeedBandwidthProber{Settings: settings, Client: &fakeMiaoSpeedRunner{}}
	if results := prober.Probe(context.Background(), storage.Node{ID: 1, Name: "node-a"}); len(results) != 0 {
		t.Fatalf("disabled prober should produce no results, got %#v", results)
	}

	settings.Enabled = true
	prober.Settings = settings
	results := prober.Probe(context.Background(), storage.Node{ID: 1, Name: "node-a"})
	if len(results) != 1 || results[0].Success || results[0].Error == nil || !strings.Contains(*results[0].Error, "raw_config") {
		t.Fatalf("expected raw_config failure, got %#v", results)
	}
}

func TestMiaoSpeedDNSLeakProberWritesNodeMetaAndResultData(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{
		frame: ms.Frame{
			ID:      "node-7-miaospeed-dns-leak",
			Version: "test-version",
			IsFinal: true,
			Nodes: []ms.NodeResult{
				{
					Name: "node-a",
					Matrices: map[string]ms.MatrixResult{
						"TEST_SCRIPT:dns_leak": {
							Type:    "TEST_SCRIPT",
							Payload: map[string]any{"Key": "dns_leak", "Text": "clean", "Servers": []any{"1.1.1.1"}},
						},
					},
				},
			},
		},
	}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.DNSLeakScript = "return 'clean'"
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	store := &fakeNodeMetaStore{}
	prober := MiaoSpeedDNSLeakProber{Settings: settings, Client: runner, Store: store}

	results := prober.Probe(context.Background(), storage.Node{ID: 7, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 || !results[0].Success || results[0].Metric != "miaospeed_dns_leak" {
		t.Fatalf("unexpected dns result: %#v", results)
	}
	if store.meta.DNSLeak == nil || *store.meta.DNSLeak != "clean" {
		t.Fatalf("expected dns leak meta clean, got %#v", store.meta)
	}
	if results[0].Data == nil || !strings.Contains(*results[0].Data, `"TEST_SCRIPT`) {
		t.Fatalf("expected raw matrix data, got %v", results[0].Data)
	}
	matrices := runner.request["Options"].(map[string]any)["Matrices"].([]map[string]string)
	if len(matrices) != 1 || matrices[0]["Type"] != "TEST_SCRIPT" || matrices[0]["Params"] != "dns_leak" {
		t.Fatalf("unexpected dns matrices: %#v", matrices)
	}
	scripts := runner.request["Configs"].(map[string]any)["Scripts"].([]map[string]any)
	if len(scripts) != 1 || scripts[0]["ID"] != "dns_leak" || scripts[0]["Content"] != "return 'clean'" {
		t.Fatalf("unexpected dns scripts: %#v", scripts)
	}
}

func TestMiaoSpeedDNSLeakProberLoadsScriptFromPath(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{
		frame: ms.Frame{
			IsFinal: true,
			Nodes: []ms.NodeResult{
				{
					Name: "node-a",
					Matrices: map[string]ms.MatrixResult{
						"TEST_SCRIPT:dns_leak": {
							Type:    "TEST_SCRIPT",
							Payload: map[string]any{"Key": "dns_leak", "Text": "clean"},
						},
					},
				},
			},
		},
	}
	scriptPath := filepath.Join(t.TempDir(), "dns-leak.js")
	if err := os.WriteFile(scriptPath, []byte("return 'path clean'"), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.DNSLeakScriptPath = scriptPath
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	prober := MiaoSpeedDNSLeakProber{Settings: settings, Client: runner, Store: &fakeNodeMetaStore{}}

	results := prober.Probe(context.Background(), storage.Node{ID: 7, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("unexpected dns result: %#v", results)
	}
	scripts := runner.request["Configs"].(map[string]any)["Scripts"].([]map[string]any)
	if len(scripts) != 1 || scripts[0]["Content"] != "return 'path clean'" {
		t.Fatalf("expected path script content, got %#v", scripts)
	}
}

func TestMiaoSpeedDNSLeakProberInlineScriptTakesPrecedenceOverPath(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{
		frame: ms.Frame{
			IsFinal: true,
			Nodes: []ms.NodeResult{
				{
					Name: "node-a",
					Matrices: map[string]ms.MatrixResult{
						"TEST_SCRIPT:dns_leak": {
							Type:    "TEST_SCRIPT",
							Payload: map[string]any{"Key": "dns_leak", "Text": "clean"},
						},
					},
				},
			},
		},
	}
	scriptPath := filepath.Join(t.TempDir(), "dns-leak.js")
	if err := os.WriteFile(scriptPath, []byte("return 'path clean'"), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.DNSLeakScript = "return 'inline clean'"
	settings.DNSLeakScriptPath = scriptPath
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	prober := MiaoSpeedDNSLeakProber{Settings: settings, Client: runner, Store: &fakeNodeMetaStore{}}

	results := prober.Probe(context.Background(), storage.Node{ID: 7, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("unexpected dns result: %#v", results)
	}
	scripts := runner.request["Configs"].(map[string]any)["Scripts"].([]map[string]any)
	if len(scripts) != 1 || scripts[0]["Content"] != "return 'inline clean'" {
		t.Fatalf("expected inline script content, got %#v", scripts)
	}
}

func TestMiaoSpeedDNSLeakProberReportsUnreadableScriptPathWithoutClientCall(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.DNSLeakScriptPath = filepath.Join(t.TempDir(), "missing.js")
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	prober := MiaoSpeedDNSLeakProber{Settings: settings, Client: runner}

	results := prober.Probe(context.Background(), storage.Node{ID: 7, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 || results[0].Success || results[0].Error == nil || !strings.Contains(*results[0].Error, "miaospeed.dns_leak_script_path") {
		t.Fatalf("expected script path failure, got %#v", results)
	}
	if runner.request != nil {
		t.Fatalf("expected no client call, got request %#v", runner.request)
	}
}

func TestMiaoSpeedUnlockProberWritesServiceUnlockMeta(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{
		frame: ms.Frame{
			ID:      "node-8-miaospeed-unlock",
			Version: "test-version",
			IsFinal: true,
			Nodes: []ms.NodeResult{
				{
					Name: "node-a",
					Matrices: map[string]ms.MatrixResult{
						"TEST_SCRIPT:netflix_unlock": {Type: "TEST_SCRIPT", Payload: map[string]any{"Key": "netflix_unlock", "Text": "full"}},
						"TEST_SCRIPT:disney_unlock":  {Type: "TEST_SCRIPT", Payload: map[string]any{"Key": "disney_unlock", "Text": "blocked"}},
						"TEST_SCRIPT:openai_unlock":  {Type: "TEST_SCRIPT", Payload: map[string]any{"Key": "openai_unlock", "Text": "available"}},
						"TEST_SCRIPT:youtube_unlock": {Type: "TEST_SCRIPT", Payload: map[string]any{"Key": "youtube_unlock", "Text": "US"}},
					},
				},
			},
		},
	}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.UnlockScripts = map[string]string{
		"netflix": "return 'full'",
		"disney":  "return 'blocked'",
		"openai":  "return 'available'",
		"youtube": "return 'US'",
	}
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	store := &fakeNodeMetaStore{}
	prober := MiaoSpeedUnlockProber{Settings: settings, Client: runner, Store: store}

	results := prober.Probe(context.Background(), storage.Node{ID: 8, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 || !results[0].Success || results[0].Metric != "miaospeed_unlock" {
		t.Fatalf("unexpected unlock result: %#v", results)
	}
	if store.meta.NetflixUnlock == nil || *store.meta.NetflixUnlock != "full" {
		t.Fatalf("unexpected netflix meta: %#v", store.meta)
	}
	if store.meta.DisneyUnlock == nil || *store.meta.DisneyUnlock != "blocked" {
		t.Fatalf("unexpected disney meta: %#v", store.meta)
	}
	if store.meta.OpenAIUnlock == nil || *store.meta.OpenAIUnlock != "available" {
		t.Fatalf("unexpected openai meta: %#v", store.meta)
	}
	if store.meta.YouTubeUnlock == nil || *store.meta.YouTubeUnlock != "US" {
		t.Fatalf("unexpected youtube meta: %#v", store.meta)
	}
	matrices := runner.request["Options"].(map[string]any)["Matrices"].([]map[string]string)
	if len(matrices) != 4 || matrices[0]["Type"] != "TEST_SCRIPT" || matrices[0]["Params"] != "netflix_unlock" {
		t.Fatalf("unexpected unlock matrices: %#v", matrices)
	}
	scripts := runner.request["Configs"].(map[string]any)["Scripts"].([]map[string]any)
	if len(scripts) != 4 || scripts[0]["ID"] != "netflix_unlock" || scripts[0]["Content"] != "return 'full'" {
		t.Fatalf("unexpected unlock scripts: %#v", scripts)
	}
}

func TestMiaoSpeedUnlockProberLoadsScriptsFromPaths(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{
		frame: ms.Frame{
			ID:      "node-8-miaospeed-unlock",
			Version: "test-version",
			IsFinal: true,
			Nodes: []ms.NodeResult{
				{
					Name: "node-a",
					Matrices: map[string]ms.MatrixResult{
						"TEST_SCRIPT:netflix_unlock": {Type: "TEST_SCRIPT", Payload: map[string]any{"Key": "netflix_unlock", "Text": "full"}},
						"TEST_SCRIPT:openai_unlock":  {Type: "TEST_SCRIPT", Payload: map[string]any{"Key": "openai_unlock", "Text": "available"}},
					},
				},
			},
		},
	}
	dir := t.TempDir()
	netflixPath := filepath.Join(dir, "netflix.js")
	openaiPath := filepath.Join(dir, "openai.js")
	if err := os.WriteFile(netflixPath, []byte("return 'netflix path'"), 0o600); err != nil {
		t.Fatalf("write netflix script: %v", err)
	}
	if err := os.WriteFile(openaiPath, []byte("return 'openai path'"), 0o600); err != nil {
		t.Fatalf("write openai script: %v", err)
	}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.UnlockScripts = map[string]string{"openai": "return 'openai inline'"}
	settings.UnlockScriptPaths = map[string]string{"netflix": netflixPath, "openai": openaiPath}
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	prober := MiaoSpeedUnlockProber{Settings: settings, Client: runner, Store: &fakeNodeMetaStore{}}

	results := prober.Probe(context.Background(), storage.Node{ID: 8, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("unexpected unlock result: %#v", results)
	}
	scripts := runner.request["Configs"].(map[string]any)["Scripts"].([]map[string]any)
	if len(scripts) != 2 {
		t.Fatalf("expected two scripts, got %#v", scripts)
	}
	if scripts[0]["ID"] != "netflix_unlock" || scripts[0]["Content"] != "return 'netflix path'" {
		t.Fatalf("expected netflix path script, got %#v", scripts)
	}
	if scripts[1]["ID"] != "openai_unlock" || scripts[1]["Content"] != "return 'openai inline'" {
		t.Fatalf("expected openai inline script precedence, got %#v", scripts)
	}
}

func TestMiaoSpeedUnlockProberReportsUnreadableScriptPathWithoutClientCall(t *testing.T) {
	runner := &fakeMiaoSpeedRunner{}
	settings := config.DefaultSettings().MiaoSpeed
	settings.Enabled = true
	settings.UnlockScriptPaths = map[string]string{"netflix": filepath.Join(t.TempDir(), "missing.js")}
	rawConfig := "name: node-a\ntype: ss\nserver: node.example.com\n"
	prober := MiaoSpeedUnlockProber{Settings: settings, Client: runner}

	results := prober.Probe(context.Background(), storage.Node{ID: 8, Name: "node-a", RawConfig: &rawConfig})
	if len(results) != 1 || results[0].Success || results[0].Error == nil || !strings.Contains(*results[0].Error, "miaospeed.unlock_script_paths.netflix") {
		t.Fatalf("expected unlock script path failure, got %#v", results)
	}
	if runner.request != nil {
		t.Fatalf("expected no client call, got request %#v", runner.request)
	}
}

type fakeMiaoSpeedRunner struct {
	request map[string]any
	frame   ms.Frame
	err     error
}

func (r *fakeMiaoSpeedRunner) Run(ctx context.Context, request map[string]any, onFrame ms.FrameHandler) (ms.Frame, error) {
	r.request = request
	if onFrame != nil {
		onFrame(r.frame)
	}
	return r.frame, r.err
}

type fakeNodeMetaStore struct {
	nodeID int
	meta   storage.NodeMeta
}

func (s *fakeNodeMetaStore) UpsertNodeMeta(nodeID int, meta storage.NodeMeta) error {
	s.nodeID = nodeID
	s.meta = meta
	return nil
}
