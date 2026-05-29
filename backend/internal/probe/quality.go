package probe

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"proxycheck/backend/internal/storage"
)

type PacketLossProber struct {
	ListenerHost string
	TimeoutMS    int
	Samples      int
	Target       TCPTarget
	Dial         Socks5DialFunc
}

func (p PacketLossProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	if node.ListenerPort == nil {
		return []storage.ProbeResultInput{failedResult("packet_loss", "tcping:default", "node listener port is not configured")}
	}
	samples := p.Samples
	if samples <= 0 {
		samples = 20
	}
	target := p.Target
	if target.Host == "" || target.Port == 0 {
		target = TCPTarget{Host: "1.1.1.1", Port: 443}
	}
	listenerHost := p.ListenerHost
	if listenerHost == "" {
		listenerHost = "127.0.0.1"
	}
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dial := p.Dial
	if dial == nil {
		dial = Socks5Connect
	}
	failed := 0
	for i := 0; i < samples; i++ {
		if err := dial(ctx, listenerHost, *node.ListenerPort, target.Host, target.Port, timeout); err != nil {
			failed++
		}
	}
	loss := (float64(failed) / float64(samples)) * 100
	dataBytes, _ := json.Marshal(map[string]int{"sent": samples, "failed": failed})
	data := string(dataBytes)
	return []storage.ProbeResultInput{
		{
			Metric:  "packet_loss",
			Target:  target.Label(),
			Value:   &loss,
			Data:    &data,
			Success: true,
		},
	}
}

type DelaySampleProvider interface {
	DelaySamples(nodeID int, limit int) ([]float64, error)
}

type JitterProber struct {
	SampleSize int
	Samples    DelaySampleProvider
}

func (p JitterProber) Probe(_ context.Context, node storage.Node) []storage.ProbeResultInput {
	if p.Samples == nil {
		return []storage.ProbeResultInput{failedResult("jitter", "delay:last_samples", "delay sample provider is not configured")}
	}
	sampleSize := p.SampleSize
	if sampleSize <= 0 {
		sampleSize = 20
	}
	values, err := p.Samples.DelaySamples(node.ID, sampleSize)
	if err != nil {
		return []storage.ProbeResultInput{failedResult("jitter", "delay:last_samples", err.Error())}
	}
	if len(values) < 2 {
		return nil
	}
	jitter := stddev(values)
	return []storage.ProbeResultInput{
		{
			Metric:  "jitter",
			Target:  "delay:last_samples",
			Value:   &jitter,
			Success: true,
		},
	}
}

func stddev(values []float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	variance := 0.0
	for _, value := range values {
		delta := value - mean
		variance += delta * delta
	}
	return math.Sqrt(variance / float64(len(values)))
}
