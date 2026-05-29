package miaospeed

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"time"
)

type SidecarOptions struct {
	Enabled        bool
	Bin            string
	Args           []string
	WorkDir        string
	WSURL          string
	Token          string
	StartTimeoutMS int
}

type SidecarManager struct {
	options     SidecarOptions
	process     *exec.Cmd
	processDone chan error
}

func NewSidecarManager(options SidecarOptions) *SidecarManager {
	return &SidecarManager{options: options}
}

func (m *SidecarManager) Start(ctx context.Context) error {
	if !m.options.Enabled {
		return nil
	}
	if m.Running() {
		return nil
	}
	if m.options.Bin == "" {
		return fmt.Errorf("miaospeed binary is not configured")
	}
	if _, err := os.Stat(m.options.Bin); err != nil {
		return fmt.Errorf("miaospeed binary not found: %s", m.options.Bin)
	}
	if m.options.WorkDir != "" {
		if err := os.MkdirAll(m.options.WorkDir, 0o755); err != nil {
			return err
		}
	}

	args, err := m.commandArgs()
	if err != nil {
		return err
	}
	env, err := m.commandEnv()
	if err != nil {
		return err
	}
	cmd := exec.Command(m.options.Bin, args...)
	if m.options.WorkDir != "" {
		cmd.Dir = m.options.WorkDir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}
	m.process = cmd
	m.processDone = make(chan error, 1)
	go func() { m.processDone <- cmd.Wait() }()
	if stdout != nil {
		go consumeSidecarOutput("miaospeed stdout", stdout)
	}
	if stderr != nil {
		go consumeSidecarOutput("miaospeed stderr", stderr)
	}
	if err := m.waitReady(ctx); err != nil {
		_ = m.Stop()
		return err
	}
	return nil
}

func (m *SidecarManager) commandArgs() ([]string, error) {
	if len(m.options.Args) > 0 {
		return append([]string{}, m.options.Args...), nil
	}
	if m.options.Token == "" {
		return nil, fmt.Errorf("miaospeed token is required when sidecar args are not configured")
	}
	return []string{"server"}, nil
}

func (m *SidecarManager) commandEnv() ([]string, error) {
	env := []string{}
	if m.options.Token != "" {
		env = append(env, "TOKEN="+m.options.Token)
	}
	if m.options.WSURL != "" {
		bind, err := bindAddressFromWSURL(m.options.WSURL)
		if err != nil {
			return nil, err
		}
		env = append(env, "BIND="+bind)
	}
	return env, nil
}

func bindAddressFromWSURL(wsURL string) (string, error) {
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("miaospeed websocket URL must include host:port")
	}
	return parsed.Host, nil
}

func (m *SidecarManager) Stop() error {
	m.observeExit()
	if m.process == nil || m.process.Process == nil || m.process.ProcessState != nil {
		m.process = nil
		m.processDone = nil
		return nil
	}
	done := m.processDone
	_ = m.process.Process.Signal(os.Interrupt)
	select {
	case <-done:
		m.process = nil
		m.processDone = nil
		return nil
	case <-time.After(5 * time.Second):
		_ = m.process.Process.Kill()
		<-done
		m.process = nil
		m.processDone = nil
		return nil
	}
}

func (m *SidecarManager) Running() bool {
	m.observeExit()
	return m.process != nil && m.process.Process != nil && m.process.ProcessState == nil
}

func (m *SidecarManager) waitReady(ctx context.Context) error {
	if m.options.WSURL == "" {
		return fmt.Errorf("miaospeed websocket URL is not configured")
	}
	timeout := time.Duration(m.options.StartTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := NewWebSocketClient(m.options.WSURL, 1000)
	var lastErr error
	for {
		if !m.Running() {
			if lastErr != nil {
				return fmt.Errorf("miaospeed exited before ready: %w", lastErr)
			}
			return fmt.Errorf("miaospeed exited before ready")
		}
		if err := client.Ping(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("miaospeed websocket not ready: %w", lastErr)
			}
			return fmt.Errorf("miaospeed websocket not ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (m *SidecarManager) observeExit() {
	if m.process == nil || m.processDone == nil {
		return
	}
	select {
	case err := <-m.processDone:
		if err != nil {
			log.Printf("miaospeed process exited: %v", err)
		}
		m.process = nil
		m.processDone = nil
	default:
	}
}

func consumeSidecarOutput(prefix string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		log.Printf("%s: %s", prefix, scanner.Text())
	}
}
