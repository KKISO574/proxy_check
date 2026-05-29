package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTcpingProberRunsTargetsThroughNodeListener(t *testing.T) {
	var calls []string
	prober := TcpingProber{
		ListenerHost: "127.0.0.1",
		TimeoutMS:    1000,
		Targets: []TCPTarget{
			{Host: "1.1.1.1", Port: 443},
			{Host: "8.8.8.8", Port: 80},
		},
		Dial: func(_ context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, _ time.Duration) error {
			calls = append(calls, listenerHost+":"+targetHost)
			if listenerPort != 20001 {
				t.Fatalf("unexpected listener port: %d", listenerPort)
			}
			if targetHost == "8.8.8.8" {
				return errors.New("connect failed")
			}
			return nil
		},
	}

	results := prober.Probe(context.Background(), testNode("node-a", 20001))
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Success || results[0].Metric != "tcping" || results[0].Target != "1.1.1.1:443" || results[0].LatencyMS == nil {
		t.Fatalf("unexpected success result: %#v", results[0])
	}
	if results[1].Success || results[1].Error == nil || results[1].Target != "8.8.8.8:80" {
		t.Fatalf("unexpected failure result: %#v", results[1])
	}
	if len(calls) != 2 {
		t.Fatalf("expected two dial calls, got %#v", calls)
	}
}

func TestTcpingProberRequiresListenerPort(t *testing.T) {
	prober := TcpingProber{
		Targets: []TCPTarget{{Host: "1.1.1.1", Port: 443}},
	}
	node := testNode("node-a", 20001)
	node.ListenerPort = nil

	results := prober.Probe(context.Background(), node)
	if len(results) != 1 || results[0].Success || results[0].Target != "tcping:default" {
		t.Fatalf("expected listener failure, got %#v", results)
	}
}
