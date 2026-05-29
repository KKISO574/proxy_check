package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTlsHandshakeProberRecordsLatency(t *testing.T) {
	prober := TlsHandshakeProber{
		ListenerHost: "127.0.0.1",
		TimeoutMS:    1000,
		Target:       TCPTarget{Host: "cp.cloudflare.com", Port: 443},
		Handshake: func(context.Context, string, int, string, int, time.Duration) (float64, error) {
			return 33.5, nil
		},
	}
	result := prober.Probe(context.Background(), testNode("node-a", 20001))[0]
	if !result.Success || result.Metric != "tls_handshake" || result.LatencyMS == nil || *result.LatencyMS != 33.5 {
		t.Fatalf("unexpected tls result: %#v", result)
	}
}

func TestHttpRttProberRecordsFailure(t *testing.T) {
	prober := HttpRttProber{
		ListenerHost: "127.0.0.1",
		TimeoutMS:    1000,
		TargetHost:   "www.gstatic.com",
		TargetPort:   443,
		Path:         "/generate_204",
		Request: func(context.Context, string, int, string, int, string, time.Duration) (float64, error) {
			return 0, errors.New("bad status")
		},
	}
	result := prober.Probe(context.Background(), testNode("node-a", 20001))[0]
	if result.Success || result.Metric != "http_rtt" || result.Error == nil {
		t.Fatalf("unexpected http failure: %#v", result)
	}
}
