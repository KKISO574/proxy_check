package miaospeed

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestMiaoSpeedSidecarIntegration(t *testing.T) {
	if os.Getenv("PROXY_CHECK_MIAOSPEED_INTEGRATION") != "1" {
		t.Skip("set PROXY_CHECK_MIAOSPEED_INTEGRATION=1 with MIAOSPEED_BIN and MIAOSPEED_TOKEN to run")
	}
	bin := os.Getenv("MIAOSPEED_BIN")
	token := os.Getenv("MIAOSPEED_TOKEN")
	if bin == "" || token == "" {
		t.Fatal("MIAOSPEED_BIN and MIAOSPEED_TOKEN are required")
	}
	wsURL := os.Getenv("MIAOSPEED_WS_URL")
	if wsURL == "" {
		wsURL = "ws://127.0.0.1:18766"
	}

	manager := NewSidecarManager(SidecarOptions{
		Enabled:        true,
		Bin:            bin,
		WSURL:          wsURL,
		Token:          token,
		StartTimeoutMS: 10000,
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start miaospeed sidecar: %v", err)
	}
	defer manager.Stop()

	client := NewWebSocketClient(
		wsURL,
		10000,
		WithToken(token),
		WithBuildTokens(splitIntegrationBuildTokens(os.Getenv("MIAOSPEED_BUILD_TOKENS"))),
	)
	request := BuildRequest(Request{
		TaskID:  "proxy-check-integration",
		Invoker: "proxy-check",
		Vendor:  "Local",
		Nodes: []Node{
			{Name: "direct", Payload: ""},
		},
		Matrices: []Matrix{
			{Type: "TEST_PING_CONN"},
		},
		Config: RequestConfig{
			PingAddress: "https://www.gstatic.com/generate_204",
			TaskTimeout: 10,
		},
	})
	frame, err := client.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("run miaospeed request: %v", err)
	}
	if !frame.IsFinal {
		t.Fatalf("expected final frame, got %#v", frame)
	}

	scriptFrame, err := client.Run(context.Background(), BuildRequest(Request{
		TaskID:  "proxy-check-script-integration",
		Invoker: "proxy-check",
		Vendor:  "Local",
		Nodes: []Node{
			{Name: "direct", Payload: ""},
		},
		Matrices: []Matrix{
			{Type: "TEST_SCRIPT", Params: "dns_leak"},
		},
		Config: RequestConfig{
			Scripts: []Script{
				{
					ID:            "dns_leak",
					Type:          "media",
					Content:       `function handler(){ return "clean"; }`,
					TimeoutMillis: 1000,
				},
			},
			TaskTimeout: 10,
		},
	}), nil)
	if err != nil {
		t.Fatalf("run miaospeed script request: %v", err)
	}
	if len(scriptFrame.Nodes) == 0 {
		t.Fatalf("expected script result node, got %#v", scriptFrame)
	}
	matrix := scriptFrame.Nodes[0].Matrices["TEST_SCRIPT:dns_leak"]
	payload, ok := matrix.Payload.(map[string]any)
	if !ok || payload["Text"] != "clean" {
		t.Fatalf("unexpected script payload: %#v", matrix.Payload)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	proxy := newIntegrationHTTPProxy(t)
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	proxyHost, proxyPort, err := net.SplitHostPort(proxyURL.Host)
	if err != nil {
		t.Fatalf("split proxy address: %v", err)
	}
	clashPayload := fmt.Sprintf("name: local-http\n"+"type: http\n"+"server: %s\n"+"port: %s\n", proxyHost, proxyPort)

	clashFrame, err := client.Run(context.Background(), BuildRequest(Request{
		TaskID:  "proxy-check-clash-integration",
		Invoker: "proxy-check",
		Vendor:  "Clash",
		Nodes: []Node{
			{Name: "local-http", Payload: clashPayload},
		},
		Matrices: []Matrix{
			{Type: "TEST_PING_CONN"},
		},
		Config: RequestConfig{
			PingAddress: target.URL,
			TaskTimeout: 10,
		},
	}), nil)
	if err != nil {
		t.Fatalf("run miaospeed clash request: %v", err)
	}
	if len(clashFrame.Nodes) == 0 {
		t.Fatalf("expected clash result node, got %#v", clashFrame)
	}
	if _, ok := clashFrame.Nodes[0].Matrices["TEST_PING_CONN"]; !ok {
		t.Fatalf("expected TEST_PING_CONN matrix, got %#v", clashFrame.Nodes[0].Matrices)
	}

	downloadTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		chunk := strings.Repeat("x", 64*1024)
		for i := 0; i < 8; i++ {
			_, _ = io.WriteString(w, chunk)
		}
	}))
	defer downloadTarget.Close()
	speedFrame, err := client.Run(context.Background(), BuildRequest(Request{
		TaskID:  "proxy-check-speed-integration",
		Invoker: "proxy-check",
		Vendor:  "Clash",
		Nodes: []Node{
			{Name: "local-http", Payload: clashPayload},
		},
		Matrices: []Matrix{
			{Type: "SPEED_AVERAGE"},
			{Type: "SPEED_MAX"},
			{Type: "SPEED_PER_SECOND"},
		},
		Config: RequestConfig{
			DownloadURL:       downloadTarget.URL,
			DownloadDuration:  1,
			DownloadThreading: 1,
			TaskTimeout:       10,
		},
	}), nil)
	if err != nil {
		t.Fatalf("run miaospeed speed request: %v", err)
	}
	if len(speedFrame.Nodes) == 0 {
		t.Fatalf("expected speed result node, got %#v", speedFrame)
	}
	if got := speedFrame.Nodes[0].AverageSpeedMbps(); got == nil || *got <= 0 {
		t.Fatalf("expected positive average speed, got %v matrices=%#v", got, speedFrame.Nodes[0].Matrices)
	}
}

func splitIntegrationBuildTokens(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "|")
}

func newIntegrationHTTPProxy(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			handleIntegrationConnect(t, w, r)
			return
		}
		if !r.URL.IsAbs() {
			http.Error(w, "absolute proxy URL required", http.StatusBadRequest)
			return
		}
		outReq := r.Clone(r.Context())
		outReq.RequestURI = ""
		resp, err := http.DefaultTransport.RoundTrip(outReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	return server
}

func handleIntegrationConnect(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}
	targetConn, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		_ = targetConn.Close()
		return
	}
	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(targetConn, clientConn)
		_ = targetConn.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(clientConn, targetConn)
		_ = clientConn.Close()
	}()
	go wg.Wait()
}
