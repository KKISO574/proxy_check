package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"proxycheck/backend/internal/storage"
)

type TLSHandshakeFunc func(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, timeout time.Duration) (float64, error)

type TlsHandshakeProber struct {
	ListenerHost string
	TimeoutMS    int
	Target       TCPTarget
	Handshake    TLSHandshakeFunc
}

func (p TlsHandshakeProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	if node.ListenerPort == nil {
		return []storage.ProbeResultInput{failedResult("tls_handshake", "tls:default", "node listener port is not configured")}
	}
	target := p.Target
	if target.Host == "" || target.Port == 0 {
		target = TCPTarget{Host: "cp.cloudflare.com", Port: 443}
	}
	handshake := p.Handshake
	if handshake == nil {
		handshake = TLSHandshakeViaSocks5
	}
	latency, err := handshake(ctx, defaultListenerHost(p.ListenerHost), *node.ListenerPort, target.Host, target.Port, timeoutDuration(p.TimeoutMS))
	if err != nil {
		return []storage.ProbeResultInput{failedResult("tls_handshake", target.Label(), err.Error())}
	}
	return []storage.ProbeResultInput{latencyResult("tls_handshake", target.Label(), latency)}
}

type HTTPRequestFunc func(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, path string, timeout time.Duration) (float64, error)

type HttpRttProber struct {
	ListenerHost string
	TimeoutMS    int
	TargetHost   string
	TargetPort   int
	Path         string
	Request      HTTPRequestFunc
}

func (p HttpRttProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	if node.ListenerPort == nil {
		return []storage.ProbeResultInput{failedResult("http_rtt", "https://www.gstatic.com/generate_204", "node listener port is not configured")}
	}
	host := p.TargetHost
	if host == "" {
		host = "www.gstatic.com"
	}
	port := p.TargetPort
	if port == 0 {
		port = 443
	}
	path := p.Path
	if path == "" {
		path = "/generate_204"
	}
	request := p.Request
	if request == nil {
		request = HTTPGetViaSocks5
	}
	latency, err := request(ctx, defaultListenerHost(p.ListenerHost), *node.ListenerPort, host, port, path, timeoutDuration(p.TimeoutMS))
	target := fmt.Sprintf("https://%s%s", net.JoinHostPort(host, strconv.Itoa(port)), path)
	if err != nil {
		return []storage.ProbeResultInput{failedResult("http_rtt", target, err.Error())}
	}
	return []storage.ProbeResultInput{latencyResult("http_rtt", target, latency)}
}

func TLSHandshakeViaSocks5(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, timeout time.Duration) (float64, error) {
	conn, err := OpenSocks5Stream(ctx, listenerHost, listenerPort, targetHost, targetPort, timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	started := time.Now()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: targetHost, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return 0, err
	}
	return float64(time.Since(started).Microseconds()) / 1000, nil
}

func HTTPGetViaSocks5(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, path string, timeout time.Duration) (float64, error) {
	conn, err := OpenSocks5Stream(ctx, listenerHost, listenerPort, targetHost, targetPort, timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: targetHost, MinVersion: tls.VersionTLS12})

	started := time.Now()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return 0, err
	}
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: proxy-check\r\nAccept: */*\r\nConnection: close\r\n\r\n", path, targetHost)
	if _, err := tlsConn.Write([]byte(request)); err != nil {
		return 0, err
	}
	statusLine, err := bufio.NewReader(tlsConn).ReadString('\n')
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(statusLine, "HTTP/") {
		return 0, fmt.Errorf("invalid HTTP response")
	}
	fields := strings.Fields(statusLine)
	if len(fields) < 2 {
		return 0, fmt.Errorf("invalid HTTP status line")
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, err
	}
	if code < 200 || code >= 400 {
		return 0, fmt.Errorf("unexpected HTTP status %d", code)
	}
	return float64(time.Since(started).Microseconds()) / 1000, nil
}

func latencyResult(metric string, target string, latency float64) storage.ProbeResultInput {
	return storage.ProbeResultInput{
		Metric:    metric,
		Target:    target,
		LatencyMS: &latency,
		Value:     &latency,
		Success:   true,
	}
}

func timeoutDuration(timeoutMS int) time.Duration {
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 {
		return 5 * time.Second
	}
	return timeout
}

func defaultListenerHost(host string) string {
	if host == "" {
		return "127.0.0.1"
	}
	return host
}
