package probe

import (
	"os"
	"strings"

	"proxycheck/backend/internal/config"
	ms "proxycheck/backend/internal/miaospeed"
)

func BuildProbers(settings config.Settings, delayClient DelayClient, dial Socks5DialFunc, sampleProviders ...DelaySampleProvider) []Prober {
	var samples DelaySampleProvider
	if len(sampleProviders) > 0 {
		samples = sampleProviders[0]
	}
	var metaStore NodeMetaStore
	if store, ok := samples.(NodeMetaStore); ok {
		metaStore = store
	}
	probers := make([]Prober, 0, len(settings.Probe.Dimensions))
	for _, dimension := range settings.Probe.Dimensions {
		switch dimension {
		case "delay":
			probers = append(probers, DelayProber{
				Client:    delayClient,
				DelayURL:  settings.Probe.DelayURL,
				TimeoutMS: settings.Probe.TimeoutMS,
			})
		case "tcping":
			targets := make([]TCPTarget, 0, len(settings.Probe.TCPTargets))
			for _, target := range settings.Probe.TCPTargets {
				targets = append(targets, TCPTarget{Host: target.Host, Port: target.Port})
			}
			probers = append(probers, TcpingProber{
				ListenerHost: settings.Mihomo.ListenerHost,
				TimeoutMS:    settings.Probe.TimeoutMS,
				Targets:      targets,
				Dial:         dial,
			})
		case "tls_handshake":
			probers = append(probers, TlsHandshakeProber{
				ListenerHost: settings.Mihomo.ListenerHost,
				TimeoutMS:    settings.Probe.TimeoutMS,
				Target:       TCPTarget{Host: "cp.cloudflare.com", Port: 443},
			})
		case "http_rtt":
			probers = append(probers, HttpRttProber{
				ListenerHost: settings.Mihomo.ListenerHost,
				TimeoutMS:    settings.Probe.TimeoutMS,
				TargetHost:   "www.gstatic.com",
				TargetPort:   443,
				Path:         "/generate_204",
			})
		case "packet_loss":
			target := TCPTarget{Host: "1.1.1.1", Port: 443}
			if len(settings.Probe.TCPTargets) > 0 {
				target = TCPTarget{Host: settings.Probe.TCPTargets[0].Host, Port: settings.Probe.TCPTargets[0].Port}
			}
			probers = append(probers, PacketLossProber{
				ListenerHost: settings.Mihomo.ListenerHost,
				TimeoutMS:    settings.Probe.TimeoutMS,
				Samples:      20,
				Target:       target,
				Dial:         dial,
			})
		case "jitter":
			probers = append(probers, JitterProber{
				SampleSize: 20,
				Samples:    samples,
			})
		case "exit_geo":
			probers = append(probers, ExitGeoProber{
				Lookup: NewSocks5GeoLookup(settings),
				Store:  metaStore,
			})
		case "miaospeed_bandwidth":
			if !settings.MiaoSpeed.Enabled {
				continue
			}
			probers = append(probers, NewMiaoSpeedBandwidthProber(
				settings.MiaoSpeed,
				newMiaoSpeedClient(settings.MiaoSpeed),
			))
		case "miaospeed_dns_leak":
			if !settings.MiaoSpeed.Enabled {
				continue
			}
			probers = append(probers, MiaoSpeedDNSLeakProber{
				Settings: settings.MiaoSpeed,
				Client:   newMiaoSpeedClient(settings.MiaoSpeed),
				Store:    metaStore,
			})
		case "miaospeed_unlock":
			if !settings.MiaoSpeed.Enabled {
				continue
			}
			probers = append(probers, MiaoSpeedUnlockProber{
				Settings: settings.MiaoSpeed,
				Client:   newMiaoSpeedClient(settings.MiaoSpeed),
				Store:    metaStore,
			})
		case "miaospeed_full":
			if !settings.MiaoSpeed.Enabled {
				continue
			}
			probers = append(probers, MiaoSpeedFullProber{
				Settings: settings.MiaoSpeed,
				Client:   newMiaoSpeedClient(settings.MiaoSpeed),
			})
		}
	}
	return probers
}

func newMiaoSpeedClient(settings config.MiaoSpeedConfig) *ms.WebSocketClient {
	return ms.NewWebSocketClient(
		settings.WSURL,
		settings.TimeoutMS,
		ms.WithToken(os.Getenv(settings.TokenEnv)),
		ms.WithBuildTokens(resolveMiaoSpeedBuildTokens(settings)),
	)
}

func resolveMiaoSpeedBuildTokens(settings config.MiaoSpeedConfig) []string {
	if settings.BuildTokenEnv != "" {
		if raw := strings.TrimSpace(os.Getenv(settings.BuildTokenEnv)); raw != "" {
			return strings.Split(raw, "|")
		}
	}
	return append([]string{}, settings.BuildTokens...)
}
