package miaospeed

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestBuildRequestUsesClashVendorNodesAndMatrices(t *testing.T) {
	request := BuildRequest(Request{
		TaskID:  "node-1-miaospeed-bandwidth",
		Invoker: "proxy-check",
		Vendor:  "Clash",
		Nodes: []Node{
			{Name: "node-a", Payload: "name: node-a\ntype: ss\n"},
		},
		Matrices: []Matrix{
			{Type: "SPEED_AVERAGE"},
			{Type: "SPEED_MAX"},
		},
		Config: RequestConfig{
			DownloadURL:       "https://example.com/10m.bin",
			DownloadDuration:  5,
			DownloadThreading: 2,
			UploadURL:         "https://example.com/upload",
			UploadDuration:    3,
			UploadThreading:   1,
			TaskTimeout:       30,
		},
	})

	basics := request["Basics"].(map[string]any)
	if basics["ID"] != "node-1-miaospeed-bandwidth" || basics["Invoker"] != "proxy-check" {
		t.Fatalf("unexpected basics: %#v", basics)
	}
	if request["Vendor"] != "Clash" {
		t.Fatalf("unexpected vendor: %#v", request["Vendor"])
	}
	nodes := request["Nodes"].([]map[string]string)
	if len(nodes) != 1 || nodes[0]["Name"] != "node-a" || nodes[0]["Payload"] == "" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	config := request["Configs"].(map[string]any)
	if config["DownloadURL"] != "https://example.com/10m.bin" || config["DownloadDuration"] != 5 {
		t.Fatalf("unexpected configs: %#v", config)
	}
	if config["ApiVersion"] != 3 || config["UploadURL"] != "https://example.com/upload" || config["UploadDuration"] != 3 {
		t.Fatalf("expected AirportR v3 config fields, got %#v", config)
	}
}

func TestBuildScriptRequestUsesMiaoSpeedScriptWireFormat(t *testing.T) {
	request := BuildRequest(Request{
		TaskID:  "node-1-miaospeed-script",
		Invoker: "proxy-check",
		Vendor:  "Clash",
		Nodes: []Node{
			{Name: "node-a", Payload: "name: node-a\n"},
		},
		Matrices: []Matrix{
			{Type: "TEST_SCRIPT", Params: "dns"},
		},
		Config: RequestConfig{
			Scripts: []Script{
				{ID: "dns", Type: "media", Content: "return 'clean'", TimeoutMillis: 1000},
			},
		},
	})

	options := request["Options"].(map[string]any)
	matrices := options["Matrices"].([]map[string]string)
	if len(matrices) != 1 || matrices[0]["Type"] != "TEST_SCRIPT" || matrices[0]["Params"] != "dns" {
		t.Fatalf("unexpected script matrix: %#v", matrices)
	}
	config := request["Configs"].(map[string]any)
	scripts := config["Scripts"].([]map[string]any)
	if len(scripts) != 1 || scripts[0]["ID"] != "dns" || scripts[0]["Type"] != "media" {
		t.Fatalf("unexpected scripts config: %#v", scripts)
	}
	if scripts[0]["TimeoutMillis"] != uint64(1000) {
		t.Fatalf("unexpected script timeout: %#v", scripts[0]["TimeoutMillis"])
	}
}

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

func TestSignRequestMatchesMiaoSpeedChallengeAlgorithm(t *testing.T) {
	request := signFixtureRequest()

	signature, err := SignRequest("server-token", []string{"build-a", "build-b"}, request)
	if err != nil {
		t.Fatalf("sign request: %v", err)
	}
	const expected = "nBDZqWTdZsoHvih8YY3mgrVcJkS5mmDF7fxvcVFL2d7qXLM-rtfHe4gEkF0WVn0ke7XymkD7V9Vx5_ZdZW2eKQ=="
	if signature != expected {
		t.Fatalf("unexpected signature\nwant %s\n got %s", expected, signature)
	}
}

func TestSignRequestIncludesEmptyBuildTokenSegmentByDefault(t *testing.T) {
	request := signFixtureRequest()
	signature, err := SignRequest("server-token", nil, request)
	if err != nil {
		t.Fatalf("sign request: %v", err)
	}
	expected := upstreamStyleSignature(t, "server-token", "", request)
	if signature != expected {
		t.Fatalf("unexpected default-build-token signature\nwant %s\n got %s", expected, signature)
	}
	explicitEmpty, err := SignRequest("server-token", []string{""}, request)
	if err != nil {
		t.Fatalf("sign request with explicit empty build token: %v", err)
	}
	if signature != explicitEmpty {
		t.Fatalf("nil build tokens should match upstream empty build token behavior\nnil=%s\nempty=%s", signature, explicitEmpty)
	}
}

func signFixtureRequest() Request {
	return Request{
		TaskID:         "task-1",
		Invoker:        "proxy-check",
		Vendor:         "Clash",
		RandomSequence: "abc",
		Nodes: []Node{
			{Name: "node-a", Payload: "name: node-a\n"},
		},
		Matrices: []Matrix{
			{Type: "TEST_SCRIPT", Params: "dns"},
		},
		Config: RequestConfig{
			Scripts: []Script{
				{ID: "dns", Type: "media", Content: "return 'ok'", TimeoutMillis: 1000},
			},
		},
	}
}

func upstreamStyleSignature(t *testing.T, token string, buildToken string, request Request) string {
	t.Helper()
	wire := request.ToWire()
	wire.Challenge = ""
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	hasher := sha512.New()
	hasher.Write(encoded)
	for _, segment := range append([]string{token}, strings.Split(strings.TrimSpace(buildToken), "|")...) {
		if segment == "" {
			segment = "SOME_TOKEN"
		}
		hasher.Write(hasher.Sum([]byte(segment)))
	}
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil))
}

func TestWebSocketClientSignsRequestWhenTokenConfigured(t *testing.T) {
	var received map[string]any
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		if err := conn.ReadJSON(&received); err != nil {
			t.Fatalf("read request: %v", err)
		}
		if received["Challenge"] == "" {
			t.Fatalf("expected signed challenge in request: %#v", received)
		}
		if err := conn.WriteJSON(map[string]any{
			"ID": "task-1",
			"Result": map[string]any{
				"Results": []any{},
			},
		}); err != nil {
			t.Fatalf("write final frame: %v", err)
		}
	}))
	defer server.Close()

	client := NewWebSocketClient(
		"ws"+server.URL[len("http"):],
		1000,
		WithToken("server-token"),
		WithBuildTokens([]string{"build-a", "build-b"}),
	)
	_, err := client.Run(context.Background(), BuildRequest(Request{
		TaskID:         "task-1",
		Invoker:        "proxy-check",
		Vendor:         "Clash",
		RandomSequence: "abc",
		Nodes: []Node{
			{Name: "node-a", Payload: "name: node-a\n"},
		},
		Matrices: []Matrix{
			{Type: "TEST_SCRIPT", Params: "dns"},
		},
		Config: RequestConfig{
			Scripts: []Script{
				{ID: "dns", Type: "media", Content: "return 'ok'", TimeoutMillis: 1000},
			},
		},
	}), nil)
	if err != nil {
		t.Fatalf("run websocket client: %v", err)
	}
	const expected = "nBDZqWTdZsoHvih8YY3mgrVcJkS5mmDF7fxvcVFL2d7qXLM-rtfHe4gEkF0WVn0ke7XymkD7V9Vx5_ZdZW2eKQ=="
	if received["Challenge"] != expected {
		t.Fatalf("unexpected signed challenge: %#v", received["Challenge"])
	}
}

func TestWebSocketClientAddsBuildTokenHintOnVerificationFailure(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		var received map[string]any
		if err := conn.ReadJSON(&received); err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := conn.WriteJSON(map[string]any{
			"Error": "cannot verify the request, please check your token",
		}); err != nil {
			t.Fatalf("write error frame: %v", err)
		}
	}))
	defer server.Close()

	client := NewWebSocketClient(
		"ws"+server.URL[len("http"):],
		1000,
		WithToken("server-token"),
	)
	_, err := client.Run(context.Background(), BuildRequest(signFixtureRequest()), nil)
	if err == nil {
		t.Fatalf("expected verification error")
	}
	if !strings.Contains(err.Error(), "MIAOSPEED_BUILD_TOKENS") {
		t.Fatalf("expected build token hint, got %v", err)
	}
}

func TestNormalizeFrameParsesFinalMatrixPayloads(t *testing.T) {
	raw := map[string]any{
		"ID":               "task-1",
		"MiaoSpeedVersion": "v1",
		"Result": map[string]any{
			"Results": []any{
				map[string]any{
					"ProxyInfo":      map[string]any{"Name": "node-a"},
					"InvokeDuration": 1234,
					"Matrices": []any{
						map[string]any{
							"Type":    "SPEED_AVERAGE",
							"Payload": `{"Value":1562500}`,
						},
						map[string]any{
							"Type":    "UDP_TYPE",
							"Payload": "full-cone",
						},
					},
				},
			},
		},
	}

	frame, err := NormalizeFrame(raw)
	if err != nil {
		t.Fatalf("normalize frame: %v", err)
	}
	if !frame.IsFinal || frame.ID != "task-1" || len(frame.Nodes) != 1 {
		t.Fatalf("unexpected frame: %#v", frame)
	}
	node := frame.Nodes[0]
	if node.Name != "node-a" || node.InvokeDurationMS != 1234 {
		t.Fatalf("unexpected node: %#v", node)
	}
	if got := node.AverageSpeedMbps(); got == nil || *got != 12.5 {
		encoded, _ := json.Marshal(node.Matrices)
		t.Fatalf("unexpected average speed %v matrices=%s", got, encoded)
	}
	if node.Matrices["UDP_TYPE"].Payload != "full-cone" {
		t.Fatalf("unexpected udp payload: %#v", node.Matrices["UDP_TYPE"])
	}
}

func TestNodeResultConvertsRealSpeedPayloadsToMbps(t *testing.T) {
	node := NodeResult{
		Matrices: map[string]MatrixResult{
			"SPEED_AVERAGE": {
				Type:    "SPEED_AVERAGE",
				Payload: map[string]any{"Value": float64(1_000_000)},
			},
			"SPEED_MAX": {
				Type:    "SPEED_MAX",
				Payload: map[string]any{"Value": float64(2_000_000)},
			},
		},
	}

	if got := node.AverageSpeedMbps(); got == nil || *got != 8 {
		t.Fatalf("unexpected average Mbps: %v", got)
	}
	if got := node.MaxSpeedMbps(); got == nil || *got != 16 {
		t.Fatalf("unexpected max Mbps: %v", got)
	}
}

func TestNormalizeFrameIndexesScriptResultsByKey(t *testing.T) {
	frame, err := NormalizeFrame(map[string]any{
		"ID": "task-1",
		"Result": map[string]any{
			"Results": []any{
				map[string]any{
					"ProxyInfo": map[string]any{"Name": "node-a"},
					"Matrices": []any{
						map[string]any{"Type": "TEST_SCRIPT", "Payload": `{"Key":"dns_leak","Text":"clean"}`},
						map[string]any{"Type": "TEST_SCRIPT", "Payload": `{"Key":"netflix_unlock","Text":"full"}`},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("normalize frame: %v", err)
	}
	node := frame.Nodes[0]
	if node.Matrices["TEST_SCRIPT:dns_leak"].Payload.(map[string]any)["Text"] != "clean" {
		t.Fatalf("missing dns script result: %#v", node.Matrices)
	}
	if node.Matrices["TEST_SCRIPT:netflix_unlock"].Payload.(map[string]any)["Text"] != "full" {
		t.Fatalf("missing netflix script result: %#v", node.Matrices)
	}
}

func TestNormalizeFrameReturnsAdapterError(t *testing.T) {
	_, err := NormalizeFrame(map[string]any{"Error": "sidecar failed"})
	if err == nil || err.Error() != "sidecar failed" {
		t.Fatalf("expected sidecar error, got %v", err)
	}
}

func TestWebSocketClientReturnsFinalFrame(t *testing.T) {
	var received map[string]any
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		if err := conn.ReadJSON(&received); err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := conn.WriteJSON(map[string]any{
			"ID":               "task-1",
			"MiaoSpeedVersion": "v-test",
			"Result": map[string]any{
				"Results": []any{
					map[string]any{
						"ProxyInfo": map[string]any{"Name": "node-a"},
						"Matrices": []any{
							map[string]any{"Type": "SPEED_AVERAGE", "Payload": `{"Value":1187500}`},
						},
					},
				},
			},
		}); err != nil {
			t.Fatalf("write final frame: %v", err)
		}
	}))
	defer server.Close()

	client := NewWebSocketClient("ws"+server.URL[len("http"):], 1000)
	frame, err := client.Run(context.Background(), map[string]any{"Vendor": "Clash"}, nil)
	if err != nil {
		t.Fatalf("run websocket client: %v", err)
	}
	if received["Vendor"] != "Clash" {
		t.Fatalf("unexpected request: %#v", received)
	}
	if !frame.IsFinal || len(frame.Nodes) != 1 {
		t.Fatalf("unexpected frame: %#v", frame)
	}
	if got := frame.Nodes[0].AverageSpeedMbps(); got == nil || *got != 9.5 {
		t.Fatalf("unexpected average speed: %v", got)
	}
}
