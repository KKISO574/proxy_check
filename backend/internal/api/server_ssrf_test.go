package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchConfigRejectsPrivateRemoteAfterPublicURLValidation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: rebound-node
    type: ss
    server: rebound.example.com
    port: 443
`))
	}))
	defer upstream.Close()

	dialLocal := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	server := &Server{opts: mergeOptions(defaultOptions(), Options{
		HTTPClient: &http.Client{
			Transport: &http.Transport{DialContext: dialLocal},
		},
	})}

	_, err := server.fetchConfig("http://93.184.216.34/config.yaml")
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("expected private remote rejection, got %v", err)
	}
}
