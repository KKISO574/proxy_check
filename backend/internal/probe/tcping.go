package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"proxycheck/backend/internal/storage"
)

type TCPTarget struct {
	Host string
	Port int
}

func (t TCPTarget) Label() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

type Socks5DialFunc func(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, timeout time.Duration) error

type TcpingProber struct {
	ListenerHost string
	TimeoutMS    int
	Targets      []TCPTarget
	Dial         Socks5DialFunc
}

func (p TcpingProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	if node.ListenerPort == nil {
		return []storage.ProbeResultInput{failedResult("tcping", "tcping:default", "node listener port is not configured")}
	}
	dial := p.Dial
	if dial == nil {
		dial = Socks5Connect
	}
	listenerHost := p.ListenerHost
	if listenerHost == "" {
		listenerHost = "127.0.0.1"
	}
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	results := make([]storage.ProbeResultInput, 0, len(p.Targets))
	for _, target := range p.Targets {
		started := time.Now()
		err := dial(ctx, listenerHost, *node.ListenerPort, target.Host, target.Port, timeout)
		latency := float64(time.Since(started).Microseconds()) / 1000
		if err != nil {
			results = append(results, failedResult("tcping", target.Label(), err.Error()))
			continue
		}
		results = append(results, storage.ProbeResultInput{
			Metric:    "tcping",
			Target:    target.Label(),
			LatencyMS: &latency,
			Value:     &latency,
			Success:   true,
		})
	}
	return results
}

func Socks5Connect(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, timeout time.Duration) error {
	conn, err := OpenSocks5Stream(ctx, listenerHost, listenerPort, targetHost, targetPort, timeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

func OpenSocks5Stream(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(listenerHost, strconv.Itoa(listenerPort)))
	if err != nil {
		cancel()
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if ok {
		_ = conn.SetDeadline(deadline)
	}

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		cancel()
		_ = conn.Close()
		return nil, err
	}
	auth := make([]byte, 2)
	if _, err := io.ReadFull(conn, auth); err != nil {
		cancel()
		_ = conn.Close()
		return nil, err
	}
	if auth[0] != 0x05 || auth[1] != 0x00 {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 authentication failed")
	}

	host := []byte(targetHost)
	if len(host) > 255 {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("target host is too long")
	}
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	request = append(request, host...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(targetPort))
	request = append(request, portBytes...)
	if _, err := conn.Write(request); err != nil {
		cancel()
		_ = conn.Close()
		return nil, err
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		cancel()
		_ = conn.Close()
		return nil, err
	}
	if header[1] != 0x00 {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 connect failed with code %d", header[1])
	}
	switch header[3] {
	case 0x01:
		if _, err := io.ReadFull(conn, make([]byte, 4+2)); err != nil {
			cancel()
			_ = conn.Close()
			return nil, err
		}
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			cancel()
			_ = conn.Close()
			return nil, err
		}
		if _, err := io.ReadFull(conn, make([]byte, int(length[0])+2)); err != nil {
			cancel()
			_ = conn.Close()
			return nil, err
		}
	case 0x04:
		if _, err := io.ReadFull(conn, make([]byte, 16+2)); err != nil {
			cancel()
			_ = conn.Close()
			return nil, err
		}
	default:
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("unsupported socks5 address type %d", header[3])
	}
	return deadlineConn{Conn: conn, cancel: cancel}, nil
}

type deadlineConn struct {
	net.Conn
	cancel context.CancelFunc
}

func (c deadlineConn) Close() error {
	c.cancel()
	return c.Conn.Close()
}
