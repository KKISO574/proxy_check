package clash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRuntimeConfigAddsControllerAndPerNodeListeners(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.yaml")
	output := filepath.Join(dir, "runtime", "config.yaml")
	if err := os.WriteFile(source, []byte(`proxies:
  - name: node-a
    type: ss
    server: a.example.com
    port: 443
  - name: node-b
    type: trojan
    server: b.example.com
    port: 8443
proxy-groups: []
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	ports, err := BuildRuntimeConfig(source, output, RuntimeOptions{
		ControllerHost: "127.0.0.1",
		ControllerPort: 9090,
		Secret:         "secret",
		ListenerHost:   "127.0.0.1",
		ListenerPorts: map[string]int{
			"node-a": 20001,
			"node-b": 20002,
		},
	})
	if err != nil {
		t.Fatalf("build runtime config: %v", err)
	}
	if ports["node-a"] != 20001 || ports["node-b"] != 20002 {
		t.Fatalf("unexpected port map: %#v", ports)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"external-controller: 127.0.0.1:9090",
		"secret: secret",
		"type: mixed",
		"proxy: node-a",
		"port: 20001",
		"proxy: node-b",
		"port: 20002",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("runtime config missing %q\n%s", want, text)
		}
	}
}
