package clash

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Node struct {
	Name      string
	Type      *string
	Server    *string
	Port      *int
	RawConfig string
}

type RuntimeOptions struct {
	ControllerHost string
	ControllerPort int
	Secret         string
	ListenerHost   string
	ListenerPorts  map[string]int
}

func LoadNodes(content []byte) ([]Node, error) {
	var root map[string]any
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	proxies, ok := root["proxies"].([]any)
	if !ok {
		return nil, fmt.Errorf("Clash config must contain a proxies list")
	}
	seen := map[string]struct{}{}
	nodes := make([]Node, 0, len(proxies))
	for _, item := range proxies {
		proxy, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, ok := proxy["name"].(string)
		if !ok || name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		raw, err := yaml.Marshal(proxy)
		if err != nil {
			return nil, fmt.Errorf("marshal proxy %q: %w", name, err)
		}
		nodes = append(nodes, Node{
			Name:      name,
			Type:      stringField(proxy, "type"),
			Server:    stringField(proxy, "server"),
			Port:      intField(proxy, "port"),
			RawConfig: string(raw),
		})
	}
	return nodes, nil
}

func BuildRuntimeConfig(sourcePath string, outputPath string, options RuntimeOptions) (map[string]int, error) {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	nodes, err := LoadNodes(content)
	if err != nil {
		return nil, err
	}
	root["external-controller"] = fmt.Sprintf("%s:%d", options.ControllerHost, options.ControllerPort)
	root["secret"] = options.Secret

	listeners, _ := root["listeners"].([]any)
	portMap := map[string]int{}
	for index, node := range nodes {
		port, ok := options.ListenerPorts[node.Name]
		if !ok {
			continue
		}
		portMap[node.Name] = port
		listeners = append(listeners, map[string]any{
			"name":   fmt.Sprintf("proxy-check-%d", index),
			"type":   "mixed",
			"listen": options.ListenerHost,
			"port":   port,
			"proxy":  node.Name,
		})
	}
	root["listeners"] = listeners

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, err
	}
	output, err := yaml.Marshal(root)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(outputPath, output, 0o600); err != nil {
		return nil, err
	}
	return portMap, nil
}

func stringField(values map[string]any, key string) *string {
	value, ok := values[key].(string)
	if !ok {
		return nil
	}
	return &value
}

func intField(values map[string]any, key string) *int {
	switch value := values[key].(type) {
	case int:
		return &value
	case int64:
		item := int(value)
		return &item
	case float64:
		item := int(value)
		return &item
	default:
		return nil
	}
}
