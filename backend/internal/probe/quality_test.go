package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPacketLossProberComputesLossPercentage(t *testing.T) {
	attempt := 0
	prober := PacketLossProber{
		ListenerHost: "127.0.0.1",
		TimeoutMS:    1000,
		Samples:      4,
		Target:       TCPTarget{Host: "1.1.1.1", Port: 443},
		Dial: func(context.Context, string, int, string, int, time.Duration) error {
			attempt++
			if attempt%2 == 0 {
				return errors.New("timeout")
			}
			return nil
		},
	}

	result := prober.Probe(context.Background(), testNode("node-a", 20001))[0]
	if !result.Success || result.Metric != "packet_loss" || result.Value == nil || *result.Value != 50 {
		t.Fatalf("unexpected packet loss result: %#v", result)
	}
	if result.Data == nil || *result.Data == "" {
		t.Fatalf("expected raw packet loss data")
	}
}

func TestJitterProberUsesRecentDelaySamples(t *testing.T) {
	prober := JitterProber{
		SampleSize: 5,
		Samples: staticDelaySamples{
			values: []float64{100, 110, 90},
		},
	}

	result := prober.Probe(context.Background(), testNode("node-a", 20001))[0]
	if !result.Success || result.Metric != "jitter" || result.Value == nil {
		t.Fatalf("unexpected jitter result: %#v", result)
	}
	if *result.Value < 8 || *result.Value > 9 {
		t.Fatalf("unexpected jitter value: %v", *result.Value)
	}
}

func TestJitterProberEmitsNoResultWithInsufficientSamples(t *testing.T) {
	prober := JitterProber{
		SampleSize: 5,
		Samples: staticDelaySamples{
			values: []float64{100},
		},
	}
	if results := prober.Probe(context.Background(), testNode("node-a", 20001)); len(results) != 0 {
		t.Fatalf("expected no result, got %#v", results)
	}
}

type staticDelaySamples struct {
	values []float64
	err    error
}

func (s staticDelaySamples) DelaySamples(_ int, _ int) ([]float64, error) {
	return s.values, s.err
}
