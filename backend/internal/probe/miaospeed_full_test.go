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
				ms.MatrixSpeedAverage:        {Type: ms.MatrixSpeedAverage, Payload: map[string]any{"Value": float64(1_250_000)}},
				ms.MatrixUSpeedAverage:       {Type: ms.MatrixUSpeedAverage, Payload: map[string]any{"Value": float64(625_000)}},
				ms.MatrixHTTPCode:            {Type: ms.MatrixHTTPCode, Payload: map[string]any{"Value": "204"}},
				ms.MatrixPacketLoss:          {Type: ms.MatrixPacketLoss, Payload: map[string]any{"Value": float64(0)}},
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
