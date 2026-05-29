package probe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"proxycheck/backend/internal/storage"
)

var errTestProbe = errors.New("probe failed")

func TestMihomoClientDelayUsesEncodedProxyNameAndAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/proxies/node%2Fa%20b/delay" {
			t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("url") != "https://cp.cloudflare.com/generate_204" {
			t.Fatalf("unexpected delay url: %q", r.URL.Query().Get("url"))
		}
		if r.URL.Query().Get("timeout") != "5000" {
			t.Fatalf("unexpected timeout: %q", r.URL.Query().Get("timeout"))
		}
		_, _ = w.Write([]byte(`{"delay":42}`))
	}))
	defer server.Close()

	client := NewMihomoClient(server.URL, "secret", 5000)
	delay, err := client.Delay(context.Background(), "node/a b", "https://cp.cloudflare.com/generate_204", 5000)
	if err != nil {
		t.Fatalf("delay failed: %v", err)
	}
	if delay != 42 {
		t.Fatalf("expected delay 42, got %v", delay)
	}
}

func TestDelayProberReturnsFailureWhenMihomoUnavailable(t *testing.T) {
	prober := DelayProber{
		Client:     failingDelayClient{err: errTestProbe},
		DelayURL:   "https://cp.cloudflare.com/generate_204",
		TimeoutMS:  5000,
		MetricName: "delay",
	}
	results := prober.Probe(context.Background(), testNode("node-a", 20001))
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].Success || results[0].Error == nil {
		t.Fatalf("expected failed result with error: %#v", results[0])
	}
}

type failingDelayClient struct {
	err error
}

func (c failingDelayClient) Delay(context.Context, string, string, int) (float64, error) {
	return 0, c.err
}

func testNode(name string, listenerPort int) storage.Node {
	return storage.Node{
		ID:           1,
		Name:         name,
		ListenerPort: &listenerPort,
		Status:       "unknown",
	}
}
