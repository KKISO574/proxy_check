package probe

import (
	"context"
	"encoding/json"
	"fmt"

	"proxycheck/backend/internal/config"
	ms "proxycheck/backend/internal/miaospeed"
	"proxycheck/backend/internal/storage"
)

type MiaoSpeedFullProber struct {
	Settings config.MiaoSpeedConfig
	Client   MiaoSpeedRunner
}

func (p MiaoSpeedFullProber) AdvancedProbe() bool {
	return true
}

func (p MiaoSpeedFullProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	const metric = "miaospeed_full"
	if !p.Settings.Enabled {
		return nil
	}
	rawConfig, failure := requireMiaoSpeedRawConfig(metric, "full", node)
	if failure != nil {
		return []storage.ProbeResultInput{*failure}
	}
	if p.Client == nil {
		return []storage.ProbeResultInput{failedResult(metric, "full", "miaospeed client is not configured")}
	}
	requestConfig, matrices := p.buildRequestParts()
	frame, err := p.Client.Run(ctx, buildMiaoSpeedNodeRequest(
		p.Settings,
		node,
		rawConfig,
		"miaospeed-full",
		matrices,
		requestConfig,
	), nil)
	if err != nil {
		return []storage.ProbeResultInput{failedResult(metric, "full", err.Error())}
	}
	result := pickMiaoSpeedNode(frame, node.Name)
	if result == nil {
		return []storage.ProbeResultInput{failedResult(metric, "full", "MiaoSpeed response did not include node result")}
	}
	data, value := buildMiaoSpeedFullResult(frame, result)
	return []storage.ProbeResultInput{{
		Metric:  metric,
		Target:  "full",
		Value:   value,
		Data:    &data,
		Success: true,
	}}
}

func (p MiaoSpeedFullProber) buildRequestParts() (ms.RequestConfig, []ms.Matrix) {
	matrices := []ms.Matrix{}
	profile := p.Settings.FullTestProfile
	if profile.IncludeDownload {
		matrices = append(matrices,
			ms.Matrix{Type: ms.MatrixSpeedAverage},
			ms.Matrix{Type: ms.MatrixSpeedMax},
			ms.Matrix{Type: ms.MatrixSpeedPerSecond},
		)
	}
	if profile.IncludeUpload {
		matrices = append(matrices,
			ms.Matrix{Type: ms.MatrixUSpeedAverage},
			ms.Matrix{Type: ms.MatrixUSpeedMax},
			ms.Matrix{Type: ms.MatrixUSpeedPerSecond},
		)
	}
	if profile.IncludeNetworkQuality {
		matrices = append(matrices,
			ms.Matrix{Type: ms.MatrixHTTPPing},
			ms.Matrix{Type: ms.MatrixRTTPing},
			ms.Matrix{Type: ms.MatrixMaxRTTPing},
			ms.Matrix{Type: ms.MatrixTotalHTTPPing},
			ms.Matrix{Type: ms.MatrixTotalRTTPing},
			ms.Matrix{Type: ms.MatrixSDRTT},
			ms.Matrix{Type: ms.MatrixSDHTTP},
			ms.Matrix{Type: ms.MatrixHTTPCode},
			ms.Matrix{Type: ms.MatrixPacketLoss},
			ms.Matrix{Type: ms.MatrixHijack},
			ms.Matrix{Type: ms.MatrixUDPType},
		)
	}
	if profile.IncludeGeo {
		matrices = append(matrices, ms.Matrix{Type: ms.MatrixInboundGeoIP}, ms.Matrix{Type: ms.MatrixOutboundGeoIP})
	}
	scripts := []ms.Script{}
	if profile.IncludeDNS {
		if content, err := resolveMiaoSpeedScript(p.Settings.DNSLeakScript, p.Settings.DNSLeakScriptPath, "miaospeed.dns_leak_script_path"); err == nil && content != "" {
			matrices = append(matrices, ms.Matrix{Type: ms.MatrixScriptTest, Params: "dns_leak"})
			scripts = append(scripts, miaoSpeedScript(p.Settings, "dns_leak", content))
		}
	}
	if profile.IncludeUnlock {
		for _, service := range ms.DefaultServiceCatalog() {
			content := p.Settings.ServiceScripts[service.Key]
			path := p.Settings.ServiceScriptPaths[service.Key]
			if content == "" && path == "" {
				continue
			}
			resolved, err := resolveMiaoSpeedScript(content, path, "miaospeed.service_script_paths."+service.Key)
			if err != nil || resolved == "" {
				continue
			}
			matrices = append(matrices, ms.Matrix{Type: ms.MatrixScriptTest, Params: service.ScriptID})
			scripts = append(scripts, miaoSpeedScript(p.Settings, service.ScriptID, resolved))
		}
	}
	return ms.RequestConfig{
		ApiVersion:        3,
		DownloadURL:       p.Settings.DownloadURL,
		DownloadDuration:  p.Settings.DownloadDurationSeconds,
		DownloadThreading: p.Settings.DownloadThreading,
		UploadURL:         p.Settings.UploadURL,
		UploadDuration:    p.Settings.UploadDurationSeconds,
		UploadThreading:   p.Settings.UploadThreading,
		TaskTimeout:       p.Settings.TaskTimeoutSeconds,
		Scripts:           scripts,
	}, matrices
}

func buildMiaoSpeedFullResult(frame ms.Frame, result *ms.NodeResult) (string, *float64) {
	download := result.AverageSpeedMbps()
	payload := map[string]any{
		"version":            frame.Version,
		"invoke_duration_ms": result.InvokeDurationMS,
		"proxy_info":         result.ProxyInfo,
		"download_mbps":      download,
		"download_max_mbps":  result.MaxSpeedMbps(),
		"upload_mbps":        result.AverageUploadMbps(),
		"upload_max_mbps":    result.MaxUploadMbps(),
		"http_code":          result.MatrixString(ms.MatrixHTTPCode, "Value", "Code", "Status"),
		"packet_loss":        result.MatrixNumber(ms.MatrixPacketLoss, "Value"),
		"hijack":             result.MatrixString(ms.MatrixHijack, "Value", "Status", "Text"),
		"udp_type":           result.MatrixString(ms.MatrixUDPType, "Value", "Type", "Text"),
		"matrices":           result.Matrices,
		"services":           extractServiceStatuses(result),
	}
	raw, _ := json.Marshal(payload)
	return string(raw), download
}

func extractServiceStatuses(result *ms.NodeResult) map[string]string {
	statuses := map[string]string{}
	for _, service := range ms.DefaultServiceCatalog() {
		matrix := result.Matrices[fmt.Sprintf("%s:%s", ms.MatrixScriptTest, service.ScriptID)]
		status := matrixStatus(matrix, "Text", "Status", "Region", "Result", "Value")
		if status != "" {
			statuses[service.Key] = status
		}
	}
	return statuses
}
