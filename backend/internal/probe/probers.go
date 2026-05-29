package probe

import (
	"context"

	"proxycheck/backend/internal/storage"
)

type DelayProber struct {
	Client     DelayClient
	DelayURL   string
	TimeoutMS  int
	MetricName string
}

func (p DelayProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	metric := p.MetricName
	if metric == "" {
		metric = "delay"
	}
	target := p.DelayURL
	if target == "" {
		target = "https://cp.cloudflare.com/generate_204"
	}
	if p.Client == nil {
		return []storage.ProbeResultInput{failedResult(metric, target, "mihomo client is not configured")}
	}
	delay, err := p.Client.Delay(ctx, node.Name, target, p.TimeoutMS)
	if err != nil {
		return []storage.ProbeResultInput{failedResult(metric, target, err.Error())}
	}
	return []storage.ProbeResultInput{
		{
			Metric:    metric,
			Target:    target,
			LatencyMS: &delay,
			Value:     &delay,
			Success:   true,
		},
	}
}

func failedResult(metric string, target string, message string) storage.ProbeResultInput {
	return storage.ProbeResultInput{
		Metric:  metric,
		Target:  target,
		Success: false,
		Error:   &message,
	}
}
