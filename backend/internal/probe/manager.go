package probe

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"proxycheck/backend/internal/clash"
	"proxycheck/backend/internal/config"
	"proxycheck/backend/internal/storage"
)

type MihomoUnavailable struct {
	Message string
}

func (e MihomoUnavailable) Error() string {
	return e.Message
}

type MihomoManager struct {
	settings          config.Settings
	process           *exec.Cmd
	processDone       chan error
	runtimeConfigPath string
	activeConfigPath  string
	activeSignature   string
}

func NewMihomoManager(settings config.Settings) *MihomoManager {
	return &MihomoManager{
		settings:          settings,
		runtimeConfigPath: filepath.Join(settings.Mihomo.WorkDir, "config.yaml"),
	}
}

func (m *MihomoManager) BeforeTaskRun(task *storage.Task, nodes []storage.Node) error {
	return m.Start(task, nodes)
}

func (m *MihomoManager) Prepare(task *storage.Task, nodes []storage.Node) error {
	sourcePath := m.sourceConfigPath(task)
	if sourcePath == "" {
		return MihomoUnavailable{Message: "mihomo source config path is not configured"}
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return MihomoUnavailable{Message: fmt.Sprintf("source config not found: %s", sourcePath)}
	}
	secret := os.Getenv(m.settings.Mihomo.SecretEnv)
	if secret == "" {
		return MihomoUnavailable{Message: fmt.Sprintf("environment variable %s is not set", m.settings.Mihomo.SecretEnv)}
	}
	listenerPorts := map[string]int{}
	for _, node := range nodes {
		if node.ListenerPort == nil || node.Status == "removed" {
			continue
		}
		listenerPorts[node.Name] = *node.ListenerPort
	}
	signature, err := m.runtimeSignature(task, nodes)
	if err != nil {
		return err
	}
	if _, err := clash.BuildRuntimeConfig(sourcePath, m.runtimeConfigPath, clash.RuntimeOptions{
		ControllerHost: m.settings.Mihomo.ControllerHost,
		ControllerPort: m.settings.Mihomo.ControllerPort,
		Secret:         secret,
		ListenerHost:   m.settings.Mihomo.ListenerHost,
		ListenerPorts:  listenerPorts,
	}); err != nil {
		return err
	}
	m.activeConfigPath = sourcePath
	m.activeSignature = signature
	return nil
}

func (m *MihomoManager) Start(task *storage.Task, nodes []storage.Node) error {
	sourcePath := m.sourceConfigPath(task)
	signature, err := m.runtimeSignature(task, nodes)
	if err != nil {
		return err
	}
	if m.isRunning() && m.activeConfigPath == sourcePath && m.activeSignature == signature {
		return nil
	}
	if m.isRunning() {
		_ = m.Stop()
	}
	if m.settings.Mihomo.Bin == "" {
		return MihomoUnavailable{Message: "mihomo binary is not configured"}
	}
	if _, err := os.Stat(m.settings.Mihomo.Bin); err != nil {
		return MihomoUnavailable{Message: fmt.Sprintf("mihomo binary not found: %s", m.settings.Mihomo.Bin)}
	}
	if err := m.Prepare(task, nodes); err != nil {
		return err
	}

	cmd := exec.Command(m.settings.Mihomo.Bin, "-f", m.runtimeConfigPath)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}
	m.process = cmd
	m.processDone = make(chan error, 1)
	go func() { m.processDone <- cmd.Wait() }()
	if stdout != nil {
		go consumeProcessOutput("mihomo stdout", stdout)
	}
	if stderr != nil {
		go consumeProcessOutput("mihomo stderr", stderr)
	}
	if err := m.waitReady(); err != nil {
		_ = m.Stop()
		return err
	}
	return nil
}

func (m *MihomoManager) Stop() error {
	m.observeProcessExit()
	if m.process == nil || m.process.Process == nil || m.process.ProcessState != nil {
		m.process = nil
		m.processDone = nil
		return nil
	}
	done := m.processDone
	_ = m.process.Process.Signal(os.Interrupt)
	select {
	case err := <-done:
		m.process = nil
		m.processDone = nil
		return err
	case <-time.After(5 * time.Second):
		_ = m.process.Process.Kill()
		err := <-done
		m.process = nil
		m.processDone = nil
		return err
	}
}

func (m *MihomoManager) isRunning() bool {
	m.observeProcessExit()
	return m.process != nil && m.process.Process != nil && m.process.ProcessState == nil
}

func (m *MihomoManager) observeProcessExit() {
	if m.process == nil || m.processDone == nil {
		return
	}
	select {
	case err := <-m.processDone:
		if err != nil {
			log.Printf("mihomo process exited: %v", err)
		}
		m.process = nil
		m.processDone = nil
	default:
	}
}

func (m *MihomoManager) waitReady() error {
	endpoint := fmt.Sprintf("http://%s:%d/version", m.settings.Mihomo.ControllerHost, m.settings.Mihomo.ControllerPort)
	client := http.Client{Timeout: 100 * time.Millisecond}
	secret := os.Getenv(m.settings.Mihomo.SecretEnv)
	var lastErr error
	for i := 0; i < 30; i++ {
		if m.process != nil && m.process.ProcessState != nil {
			return MihomoUnavailable{Message: fmt.Sprintf("mihomo exited before ready: %v", m.process.ProcessState)}
		}
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
		response, err := client.Do(req)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("controller returned status %d", response.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return MihomoUnavailable{Message: fmt.Sprintf("controller not ready within 3.0s: %v", lastErr)}
}

func (m *MihomoManager) sourceConfigPath(task *storage.Task) string {
	if task != nil && task.ConfigPath != "" {
		return task.ConfigPath
	}
	return m.settings.Mihomo.SourceConfigPath
}

func (m *MihomoManager) runtimeSignature(task *storage.Task, nodes []storage.Node) (string, error) {
	sourcePath := m.sourceConfigPath(task)
	if sourcePath == "" {
		return "", MihomoUnavailable{Message: "mihomo source config path is not configured"}
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", MihomoUnavailable{Message: fmt.Sprintf("source config not found: %s", sourcePath)}
	}
	secret := os.Getenv(m.settings.Mihomo.SecretEnv)
	ports := map[string]int{}
	for _, node := range nodes {
		if node.ListenerPort == nil || node.Status == "removed" {
			continue
		}
		ports[node.Name] = *node.ListenerPort
	}
	payload := map[string]any{
		"config":          string(content),
		"controller_host": m.settings.Mihomo.ControllerHost,
		"controller_port": m.settings.Mihomo.ControllerPort,
		"listener_host":   m.settings.Mihomo.ListenerHost,
		"listener_ports":  ports,
		"secret":          secret,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func consumeProcessOutput(prefix string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		log.Printf("%s: %s", prefix, scanner.Text())
	}
}
