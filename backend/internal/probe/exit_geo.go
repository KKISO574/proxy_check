package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"proxycheck/backend/internal/config"
	"proxycheck/backend/internal/storage"
)

type NodeMetaUpdate = storage.NodeMeta

type GeoResult struct {
	ExitIP  string
	ASN     string
	Country string
	Region  string
	ISP     string
}

type GeoLookupFunc func(ctx context.Context, node storage.Node) (GeoResult, error)

type NodeMetaStore interface {
	UpsertNodeMeta(nodeID int, meta NodeMetaUpdate) error
}

type ExitGeoProber struct {
	Lookup GeoLookupFunc
	Store  NodeMetaStore
}

func NewSocks5GeoLookup(settings config.Settings) GeoLookupFunc {
	return func(ctx context.Context, node storage.Node) (GeoResult, error) {
		if node.ListenerPort == nil {
			return GeoResult{}, fmt.Errorf("node listener port is not configured")
		}
		timeout := timeoutDuration(settings.Probe.TimeoutMS)
		endpoints := []struct {
			host string
			path string
		}{
			{host: "ipapi.co", path: "/json"},
			{host: "api.ip.sb", path: "/geoip"},
		}
		var lastErr error
		for _, endpoint := range endpoints {
			payload, err := HTTPSJSONViaSocks5(ctx, defaultListenerHost(settings.Mihomo.ListenerHost), *node.ListenerPort, endpoint.host, 443, endpoint.path, timeout)
			if err != nil {
				lastErr = err
				continue
			}
			return geoFromPayload(payload), nil
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("exit geo lookup failed")
		}
		return GeoResult{}, lastErr
	}
}

func (p ExitGeoProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	if p.Lookup == nil {
		return []storage.ProbeResultInput{failedResult("exit_geo", "https://ipapi.co/json", "exit geo lookup is not configured")}
	}
	result, err := p.Lookup(ctx, node)
	if err != nil {
		return []storage.ProbeResultInput{failedResult("exit_geo", "https://ipapi.co/json", err.Error())}
	}
	meta := NodeMetaUpdate{
		ExitIP:  stringPointer(result.ExitIP),
		ASN:     stringPointer(result.ASN),
		Country: stringPointer(result.Country),
		Region:  stringPointer(result.Region),
		ISP:     stringPointer(result.ISP),
	}
	if p.Store != nil {
		if err := p.Store.UpsertNodeMeta(node.ID, meta); err != nil {
			return []storage.ProbeResultInput{failedResult("exit_geo", "https://ipapi.co/json", err.Error())}
		}
	}
	dataBytes, _ := json.Marshal(result)
	data := string(dataBytes)
	return []storage.ProbeResultInput{
		{
			Metric:  "exit_geo",
			Target:  "https://ipapi.co/json",
			Data:    &data,
			Success: true,
		},
	}
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func HTTPSJSONViaSocks5(ctx context.Context, listenerHost string, listenerPort int, targetHost string, targetPort int, path string, timeout time.Duration) (map[string]any, error) {
	conn, err := OpenSocks5Stream(ctx, listenerHost, listenerPort, targetHost, targetPort, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: targetHost, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: proxy-check\r\nAccept: application/json\r\nConnection: close\r\n\r\n", path, targetHost)
	if _, err := tlsConn.Write([]byte(request)); err != nil {
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func geoFromPayload(payload map[string]any) GeoResult {
	return GeoResult{
		ExitIP:  stringValue(payload, "ip"),
		ASN:     firstStringValue(payload, "asn", "as"),
		Country: firstStringValue(payload, "country_code", "country"),
		Region:  firstStringValue(payload, "region", "region_name"),
		ISP:     firstStringValue(payload, "org", "isp", "organization"),
	}
}

func firstStringValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value := stringValue(payload, key)
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(payload map[string]any, key string) string {
	switch value := payload[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatInt(int64(value), 10)
	case int:
		return strconv.Itoa(value)
	default:
		return ""
	}
}
