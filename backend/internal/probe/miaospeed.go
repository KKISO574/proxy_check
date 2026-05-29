package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"proxycheck/backend/internal/config"
	ms "proxycheck/backend/internal/miaospeed"
	"proxycheck/backend/internal/storage"
)

const (
	miaoSpeedScriptMatrix = "TEST_SCRIPT"
	miaoSpeedMediaScript  = "media"
)

type MiaoSpeedRunner interface {
	Run(ctx context.Context, request map[string]any, onFrame ms.FrameHandler) (ms.Frame, error)
}

type MiaoSpeedBandwidthProber struct {
	Settings  config.MiaoSpeedConfig
	Client    MiaoSpeedRunner
	semaphore chan struct{}
}

type MiaoSpeedDNSLeakProber struct {
	Settings config.MiaoSpeedConfig
	Client   MiaoSpeedRunner
	Store    NodeMetaStore
}

type MiaoSpeedUnlockProber struct {
	Settings config.MiaoSpeedConfig
	Client   MiaoSpeedRunner
	Store    NodeMetaStore
}

func NewMiaoSpeedBandwidthProber(settings config.MiaoSpeedConfig, client MiaoSpeedRunner) MiaoSpeedBandwidthProber {
	limit := settings.MaxBandwidthConcurrency
	if limit <= 0 {
		limit = 1
	}
	return MiaoSpeedBandwidthProber{
		Settings:  settings,
		Client:    client,
		semaphore: make(chan struct{}, limit),
	}
}

func (p MiaoSpeedBandwidthProber) AdvancedProbe() bool {
	return true
}

func (p MiaoSpeedDNSLeakProber) AdvancedProbe() bool {
	return true
}

func (p MiaoSpeedUnlockProber) AdvancedProbe() bool {
	return true
}

func (p MiaoSpeedBandwidthProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	const metric = "miaospeed_bandwidth"
	if !p.Settings.Enabled {
		return nil
	}
	if p.Settings.DownloadURL == "" {
		return []storage.ProbeResultInput{failedResult(metric, "", "miaospeed.download_url is required")}
	}
	if node.RawConfig == nil || *node.RawConfig == "" {
		return []storage.ProbeResultInput{failedResult(metric, p.Settings.DownloadURL, "node raw_config is required for MiaoSpeed Clash vendor")}
	}
	if p.Client == nil {
		return []storage.ProbeResultInput{failedResult(metric, p.Settings.DownloadURL, "miaospeed client is not configured")}
	}

	if p.semaphore != nil {
		select {
		case p.semaphore <- struct{}{}:
			defer func() { <-p.semaphore }()
		case <-ctx.Done():
			return []storage.ProbeResultInput{failedResult(metric, p.Settings.DownloadURL, ctx.Err().Error())}
		}
	}

	request := ms.BuildRequest(ms.Request{
		TaskID:  fmt.Sprintf("node-%d-miaospeed-bandwidth", node.ID),
		Invoker: p.Settings.Invoker,
		Vendor:  "Clash",
		Nodes: []ms.Node{
			{Name: node.Name, Payload: *node.RawConfig},
		},
		Matrices: []ms.Matrix{
			{Type: "SPEED_AVERAGE"},
			{Type: "SPEED_MAX"},
			{Type: "SPEED_PER_SECOND"},
		},
		Config: ms.RequestConfig{
			DownloadURL:       p.Settings.DownloadURL,
			DownloadDuration:  p.Settings.DownloadDurationSeconds,
			DownloadThreading: p.Settings.DownloadThreading,
			TaskTimeout:       p.Settings.TaskTimeoutSeconds,
		},
	})
	frame, err := p.Client.Run(ctx, request, nil)
	if err != nil {
		return []storage.ProbeResultInput{failedResult(metric, p.Settings.DownloadURL, err.Error())}
	}
	result := pickMiaoSpeedNode(frame, node.Name)
	if result == nil {
		return []storage.ProbeResultInput{failedResult(metric, p.Settings.DownloadURL, "MiaoSpeed response did not include node result")}
	}
	average := result.AverageSpeedMbps()
	if average == nil {
		return []storage.ProbeResultInput{failedResult(metric, p.Settings.DownloadURL, "MiaoSpeed response did not include average speed")}
	}
	maxSpeed := result.MaxSpeedMbps()
	dataBytes, _ := json.Marshal(map[string]any{
		"version":            frame.Version,
		"average_mbps":       *average,
		"max_mbps":           maxSpeed,
		"invoke_duration_ms": result.InvokeDurationMS,
		"proxy_info":         result.ProxyInfo,
		"matrices":           result.Matrices,
	})
	data := string(dataBytes)
	return []storage.ProbeResultInput{
		{
			Metric:  metric,
			Target:  p.Settings.DownloadURL,
			Value:   average,
			Data:    &data,
			Success: true,
		},
	}
}

func (p MiaoSpeedDNSLeakProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	const metric = "miaospeed_dns_leak"
	if !p.Settings.Enabled {
		return nil
	}
	rawConfig, failure := requireMiaoSpeedRawConfig(metric, "dns_leak", node)
	if failure != nil {
		return []storage.ProbeResultInput{*failure}
	}
	if p.Client == nil {
		return []storage.ProbeResultInput{failedResult(metric, "dns_leak", "miaospeed client is not configured")}
	}
	scriptContent, err := resolveMiaoSpeedScript(p.Settings.DNSLeakScript, p.Settings.DNSLeakScriptPath, "miaospeed.dns_leak_script_path")
	if err != nil {
		return []storage.ProbeResultInput{failedResult(metric, "dns_leak", err.Error())}
	}
	if scriptContent == "" {
		return []storage.ProbeResultInput{failedResult(metric, "dns_leak", "miaospeed.dns_leak_script is required")}
	}
	frame, err := p.Client.Run(ctx, buildMiaoSpeedNodeRequest(
		p.Settings,
		node,
		rawConfig,
		"miaospeed-dns-leak",
		[]ms.Matrix{{Type: miaoSpeedScriptMatrix, Params: "dns_leak"}},
		ms.RequestConfig{Scripts: []ms.Script{miaoSpeedScript(p.Settings, "dns_leak", scriptContent)}},
	), nil)
	if err != nil {
		return []storage.ProbeResultInput{failedResult(metric, "dns_leak", err.Error())}
	}
	result := pickMiaoSpeedNode(frame, node.Name)
	if result == nil {
		return []storage.ProbeResultInput{failedResult(metric, "dns_leak", "MiaoSpeed response did not include node result")}
	}
	status := matrixStatus(scriptMatrix(result, "dns_leak"), "Text", "Status", "Result", "Value")
	if status == "" {
		return []storage.ProbeResultInput{failedResult(metric, "dns_leak", "MiaoSpeed response did not include DNS leak status")}
	}
	if p.Store != nil {
		if err := p.Store.UpsertNodeMeta(node.ID, storage.NodeMeta{DNSLeak: &status}); err != nil {
			return []storage.ProbeResultInput{failedResult(metric, "dns_leak", err.Error())}
		}
	}
	data := miaoSpeedResultData(frame, result)
	return []storage.ProbeResultInput{{Metric: metric, Target: "dns_leak", Data: &data, Success: true}}
}

func (p MiaoSpeedUnlockProber) Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput {
	const metric = "miaospeed_unlock"
	if !p.Settings.Enabled {
		return nil
	}
	rawConfig, failure := requireMiaoSpeedRawConfig(metric, "unlock", node)
	if failure != nil {
		return []storage.ProbeResultInput{*failure}
	}
	if p.Client == nil {
		return []storage.ProbeResultInput{failedResult(metric, "unlock", "miaospeed client is not configured")}
	}
	requestMatrices := []ms.Matrix{}
	requestScripts := []ms.Script{}
	for _, item := range unlockScriptDefinitions() {
		content, err := resolveMiaoSpeedScript(
			p.Settings.UnlockScripts[item.configKey],
			p.Settings.UnlockScriptPaths[item.configKey],
			"miaospeed.unlock_script_paths."+item.configKey,
		)
		if err != nil {
			return []storage.ProbeResultInput{failedResult(metric, "unlock", err.Error())}
		}
		if content == "" {
			continue
		}
		requestMatrices = append(requestMatrices, ms.Matrix{Type: miaoSpeedScriptMatrix, Params: item.scriptID})
		requestScripts = append(requestScripts, miaoSpeedScript(p.Settings, item.scriptID, content))
	}
	if len(requestScripts) == 0 {
		return []storage.ProbeResultInput{failedResult(metric, "unlock", "miaospeed.unlock_scripts is required")}
	}
	frame, err := p.Client.Run(ctx, buildMiaoSpeedNodeRequest(
		p.Settings,
		node,
		rawConfig,
		"miaospeed-unlock",
		requestMatrices,
		ms.RequestConfig{Scripts: requestScripts},
	), nil)
	if err != nil {
		return []storage.ProbeResultInput{failedResult(metric, "unlock", err.Error())}
	}
	result := pickMiaoSpeedNode(frame, node.Name)
	if result == nil {
		return []storage.ProbeResultInput{failedResult(metric, "unlock", "MiaoSpeed response did not include node result")}
	}
	meta := storage.NodeMeta{
		NetflixUnlock: stringPointer(matrixStatus(scriptMatrix(result, "netflix_unlock"), "Text", "Status", "Result", "Value")),
		DisneyUnlock:  stringPointer(matrixStatus(scriptMatrix(result, "disney_unlock"), "Text", "Status", "Result", "Value")),
		OpenAIUnlock:  stringPointer(matrixStatus(scriptMatrix(result, "openai_unlock"), "Text", "Status", "Result", "Value")),
		YouTubeUnlock: stringPointer(matrixStatus(scriptMatrix(result, "youtube_unlock"), "Text", "Region", "Status", "Result", "Value")),
	}
	if meta.NetflixUnlock == nil && meta.DisneyUnlock == nil && meta.OpenAIUnlock == nil && meta.YouTubeUnlock == nil {
		return []storage.ProbeResultInput{failedResult(metric, "unlock", "MiaoSpeed response did not include unlock statuses")}
	}
	if p.Store != nil {
		if err := p.Store.UpsertNodeMeta(node.ID, meta); err != nil {
			return []storage.ProbeResultInput{failedResult(metric, "unlock", err.Error())}
		}
	}
	data := miaoSpeedResultData(frame, result)
	return []storage.ProbeResultInput{{Metric: metric, Target: "unlock", Data: &data, Success: true}}
}

type unlockScriptDef struct {
	configKey string
	scriptID  string
}

func unlockScriptDefinitions() []unlockScriptDef {
	return []unlockScriptDef{
		{configKey: "netflix", scriptID: "netflix_unlock"},
		{configKey: "disney", scriptID: "disney_unlock"},
		{configKey: "openai", scriptID: "openai_unlock"},
		{configKey: "youtube", scriptID: "youtube_unlock"},
	}
}

func resolveMiaoSpeedScript(inline string, path string, fieldName string) (string, error) {
	if content := strings.TrimSpace(inline); content != "" {
		return content, nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fieldName, err)
	}
	return strings.TrimSpace(string(content)), nil
}

func miaoSpeedScript(settings config.MiaoSpeedConfig, id string, content string) ms.Script {
	timeout := settings.ScriptTimeoutMS
	if timeout <= 0 {
		timeout = 10000
	}
	return ms.Script{
		ID:            id,
		Type:          miaoSpeedMediaScript,
		Content:       content,
		TimeoutMillis: uint64(timeout),
	}
}

func scriptMatrix(result *ms.NodeResult, key string) ms.MatrixResult {
	if result == nil {
		return ms.MatrixResult{}
	}
	if matrix, ok := result.Matrices[miaoSpeedScriptMatrix+":"+key]; ok {
		return matrix
	}
	return result.Matrices[miaoSpeedScriptMatrix]
}

func pickMiaoSpeedNode(frame ms.Frame, nodeName string) *ms.NodeResult {
	for i := range frame.Nodes {
		if frame.Nodes[i].Name == nodeName {
			return &frame.Nodes[i]
		}
	}
	if len(frame.Nodes) > 0 {
		return &frame.Nodes[0]
	}
	return nil
}

func requireMiaoSpeedRawConfig(metric string, target string, node storage.Node) (string, *storage.ProbeResultInput) {
	if node.RawConfig == nil || *node.RawConfig == "" {
		result := failedResult(metric, target, "node raw_config is required for MiaoSpeed Clash vendor")
		return "", &result
	}
	return *node.RawConfig, nil
}

func buildMiaoSpeedNodeRequest(settings config.MiaoSpeedConfig, node storage.Node, rawConfig string, taskSuffix string, matrices []ms.Matrix, requestConfig ms.RequestConfig) map[string]any {
	return ms.BuildRequest(ms.Request{
		TaskID:  fmt.Sprintf("node-%d-%s", node.ID, taskSuffix),
		Invoker: settings.Invoker,
		Vendor:  "Clash",
		Nodes: []ms.Node{
			{Name: node.Name, Payload: rawConfig},
		},
		Matrices: matrices,
		Config:   requestConfig,
	})
}

func matrixStatus(matrix ms.MatrixResult, keys ...string) string {
	switch payload := matrix.Payload.(type) {
	case string:
		return payload
	case map[string]any:
		for _, key := range keys {
			if value, ok := payload[key]; ok {
				switch item := value.(type) {
				case string:
					return item
				case float64:
					return fmt.Sprintf("%.0f", item)
				case int:
					return fmt.Sprintf("%d", item)
				}
			}
		}
	}
	return ""
}

func miaoSpeedResultData(frame ms.Frame, result *ms.NodeResult) string {
	dataBytes, _ := json.Marshal(map[string]any{
		"version":            frame.Version,
		"invoke_duration_ms": result.InvokeDurationMS,
		"proxy_info":         result.ProxyInfo,
		"matrices":           result.Matrices,
	})
	return string(dataBytes)
}
