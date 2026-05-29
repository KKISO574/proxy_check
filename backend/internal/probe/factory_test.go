package probe

import (
	"testing"

	"proxycheck/backend/internal/config"
	ms "proxycheck/backend/internal/miaospeed"
)

func TestBuildProbersUsesConfiguredDimensionsAndTargets(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Probe.Dimensions = []string{"tcping", "tls_handshake", "http_rtt", "packet_loss", "jitter", "exit_geo", "miaospeed_bandwidth", "miaospeed_dns_leak", "miaospeed_unlock", "delay"}
	settings.Probe.TCPTargets = []config.TCPTarget{{Host: "9.9.9.9", Port: 443}}
	settings.Mihomo.ListenerHost = "127.0.0.2"
	settings.MiaoSpeed.Enabled = true

	probers := BuildProbers(settings, failingDelayClient{err: errTestProbe}, nil, staticDelaySamples{})
	if len(probers) != 10 {
		t.Fatalf("expected 10 probers, got %d", len(probers))
	}
	if _, ok := probers[0].(TcpingProber); !ok {
		t.Fatalf("expected tcping prober first, got %T", probers[0])
	}
	if _, ok := probers[1].(TlsHandshakeProber); !ok {
		t.Fatalf("expected tls prober second, got %T", probers[1])
	}
	if _, ok := probers[2].(HttpRttProber); !ok {
		t.Fatalf("expected http prober third, got %T", probers[2])
	}
	if _, ok := probers[3].(PacketLossProber); !ok {
		t.Fatalf("expected packet loss prober fourth, got %T", probers[3])
	}
	if _, ok := probers[4].(JitterProber); !ok {
		t.Fatalf("expected jitter prober fifth, got %T", probers[4])
	}
	if _, ok := probers[5].(ExitGeoProber); !ok {
		t.Fatalf("expected exit geo prober sixth, got %T", probers[5])
	}
	if _, ok := probers[6].(MiaoSpeedBandwidthProber); !ok {
		t.Fatalf("expected miaospeed bandwidth prober seventh, got %T", probers[6])
	}
	if _, ok := probers[7].(MiaoSpeedDNSLeakProber); !ok {
		t.Fatalf("expected miaospeed dns leak prober eighth, got %T", probers[7])
	}
	if _, ok := probers[8].(MiaoSpeedUnlockProber); !ok {
		t.Fatalf("expected miaospeed unlock prober ninth, got %T", probers[8])
	}
	if _, ok := probers[9].(DelayProber); !ok {
		t.Fatalf("expected delay prober tenth, got %T", probers[9])
	}
	tcping := probers[0].(TcpingProber)
	if tcping.ListenerHost != "127.0.0.2" || len(tcping.Targets) != 1 || tcping.Targets[0].Host != "9.9.9.9" {
		t.Fatalf("unexpected tcping prober: %#v", tcping)
	}
}

func TestBuildProbersConfiguresMiaoSpeedClientSigning(t *testing.T) {
	t.Setenv("TEST_MIAOSPEED_TOKEN", "server-token")
	t.Setenv("TEST_MIAOSPEED_BUILD_TOKENS", "env-build-a|env-build-b")
	settings := config.DefaultSettings()
	settings.Probe.Dimensions = []string{"miaospeed_bandwidth"}
	settings.MiaoSpeed.Enabled = true
	settings.MiaoSpeed.WSURL = "ws://127.0.0.1:8766"
	settings.MiaoSpeed.TokenEnv = "TEST_MIAOSPEED_TOKEN"
	settings.MiaoSpeed.BuildTokenEnv = "TEST_MIAOSPEED_BUILD_TOKENS"
	settings.MiaoSpeed.BuildTokens = []string{"config-build"}

	probers := BuildProbers(settings, failingDelayClient{err: errTestProbe}, nil, staticDelaySamples{})
	if len(probers) != 1 {
		t.Fatalf("expected one prober, got %d", len(probers))
	}
	prober := probers[0].(MiaoSpeedBandwidthProber)
	client, ok := prober.Client.(*ms.WebSocketClient)
	if !ok {
		t.Fatalf("expected websocket client, got %T", prober.Client)
	}
	if client.Token != "server-token" {
		t.Fatalf("unexpected token: %q", client.Token)
	}
	if len(client.BuildTokens) != 2 || client.BuildTokens[0] != "env-build-a" || client.BuildTokens[1] != "env-build-b" {
		t.Fatalf("unexpected build tokens: %#v", client.BuildTokens)
	}
}

func TestBuildProbersRegistersMiaoSpeedFullWhenEnabled(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Probe.Dimensions = []string{"miaospeed_full"}
	settings.MiaoSpeed.Enabled = true

	probers := BuildProbers(settings, failingDelayClient{err: errTestProbe}, nil, staticDelaySamples{})
	if len(probers) != 1 {
		t.Fatalf("expected one prober, got %d", len(probers))
	}
	if _, ok := probers[0].(MiaoSpeedFullProber); !ok {
		t.Fatalf("expected miaospeed full prober, got %T", probers[0])
	}
}

func TestBuildProbersSkipsMiaoSpeedDimensionsWhenDisabled(t *testing.T) {
	settings := config.DefaultSettings()
	settings.Probe.Dimensions = []string{"delay", "miaospeed_bandwidth", "miaospeed_dns_leak", "miaospeed_unlock", "miaospeed_full"}
	settings.MiaoSpeed.Enabled = false

	probers := BuildProbers(settings, failingDelayClient{err: errTestProbe}, nil, staticDelaySamples{})
	if len(probers) != 1 {
		t.Fatalf("expected only non-MiaoSpeed prober when disabled, got %d: %#v", len(probers), probers)
	}
	if _, ok := probers[0].(DelayProber); !ok {
		t.Fatalf("expected delay prober, got %T", probers[0])
	}
}
