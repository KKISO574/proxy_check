package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"proxycheck/backend/internal/clash"
	"proxycheck/backend/internal/storage"
)

type Repository interface {
	Tasks() ([]storage.Task, error)
	GetTask(id int) (*storage.Task, error)
	CreateTask(name, sourceURL, configPath string, intervalSeconds int, enabled bool, advancedProbesEnabled bool) (*storage.Task, error)
	UpdateTask(id int, patch storage.TaskPatch) (*storage.Task, error)
	DeleteTask(id int) error
	SyncNodes(taskID int, inputs []storage.NodeInput, portStart, portMax int) ([]storage.Node, error)
	Nodes(taskID *int) ([]storage.Node, error)
	Node(id int) (*storage.Node, error)
	Stats(taskID *int) (storage.Stats, error)
	History(nodeID int, metric string, rangeName string) ([]storage.MetricSummary, error)
	RecentErrors(nodeID int, limit int) ([]storage.MetricSummary, error)
}

type ProbeRunner interface {
	RunTask(taskID int) (RunSummary, error)
	RunAll() (RunSummary, error)
}

type RunSummary = storage.RunSummary

type Options struct {
	ConfigDir              string
	StaticDir              string
	ListenerPortStart      int
	ListenerPortMax        int
	HTTPClient             *http.Client
	Runner                 ProbeRunner
	AllowPrivateConfigURLs bool
}

type taskCreateRequest struct {
	Name                  string `json:"name"`
	SourceURL             string `json:"source_url"`
	IntervalSeconds       int    `json:"interval_seconds"`
	Enabled               *bool  `json:"enabled"`
	AdvancedProbesEnabled *bool  `json:"advanced_probes_enabled"`
}

type taskUpdateRequest struct {
	Name                  *string `json:"name"`
	SourceURL             *string `json:"source_url"`
	IntervalSeconds       *int    `json:"interval_seconds"`
	Enabled               *bool   `json:"enabled"`
	AdvancedProbesEnabled *bool   `json:"advanced_probes_enabled"`
}

type taskImportResponse struct {
	Task  storage.Task `json:"task"`
	Nodes int          `json:"nodes"`
}

type nodeDetailResponse struct {
	storage.Node
	RecentErrors []storage.MetricSummary `json:"recent_errors"`
}

type Server struct {
	repo Repository
	opts Options
	mux  *http.ServeMux
}

func NewServer(repo Repository, optionList ...Options) http.Handler {
	opts := defaultOptions()
	if len(optionList) > 0 {
		opts = mergeOptions(opts, optionList[0])
	}
	server := &Server{
		repo: repo,
		opts: opts,
		mux:  http.NewServeMux(),
	}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	repo := s.repo
	s.mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		tasks, err := repo.Tasks()
		writeJSON(w, tasks, err)
	})
	s.mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var payload taskCreateRequest
		if !decodeJSON(w, r, &payload) {
			return
		}
		if payload.IntervalSeconds == 0 {
			payload.IntervalSeconds = 60
		}
		enabled := true
		if payload.Enabled != nil {
			enabled = *payload.Enabled
		}
		advancedProbesEnabled := false
		if payload.AdvancedProbesEnabled != nil {
			advancedProbesEnabled = *payload.AdvancedProbesEnabled
		}
		task, nodes, err := s.createImportedTask(payload.Name, payload.SourceURL, payload.IntervalSeconds, enabled, advancedProbesEnabled)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSONValue(w, taskImportResponse{Task: *task, Nodes: len(nodes)})
	})
	s.mux.HandleFunc("PATCH /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathInt(w, r, "id")
		if !ok {
			return
		}
		var payload taskUpdateRequest
		if !decodeJSON(w, r, &payload) {
			return
		}
		task, err := repo.GetTask(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if task == nil {
			http.NotFound(w, r)
			return
		}
		sourceChanged := payload.SourceURL != nil && *payload.SourceURL != task.SourceURL
		updated, err := repo.UpdateTask(id, storage.TaskPatch{
			Name:                  payload.Name,
			SourceURL:             payload.SourceURL,
			Enabled:               payload.Enabled,
			IntervalSeconds:       payload.IntervalSeconds,
			AdvancedProbesEnabled: payload.AdvancedProbesEnabled,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		nodes, err := repo.Nodes(&id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if sourceChanged {
			updated, nodes, err = s.refreshTask(id)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		writeJSONValue(w, taskImportResponse{Task: *updated, Nodes: len(nodes)})
	})
	s.mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathInt(w, r, "id")
		if !ok {
			return
		}
		task, err := repo.GetTask(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if task == nil {
			http.NotFound(w, r)
			return
		}
		if err := repo.DeleteTask(id); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s.mux.HandleFunc("POST /api/tasks/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathInt(w, r, "id")
		if !ok {
			return
		}
		task, nodes, err := s.refreshTask(id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSONValue(w, taskImportResponse{Task: *task, Nodes: len(nodes)})
	})
	s.mux.HandleFunc("GET /api/nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes, err := repo.Nodes(optionalInt(r.URL.Query().Get("task_id")))
		writeJSON(w, nodes, err)
	})
	s.mux.HandleFunc("GET /api/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathInt(w, r, "id")
		if !ok {
			return
		}
		node, err := repo.Node(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if node == nil {
			http.NotFound(w, r)
			return
		}
		errors, err := repo.RecentErrors(id, 20)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSONValue(w, nodeDetailResponse{Node: *node, RecentErrors: errors})
	})
	s.mux.HandleFunc("GET /api/nodes/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathInt(w, r, "id")
		if !ok {
			return
		}
		metric := r.URL.Query().Get("metric")
		if metric == "" {
			http.Error(w, "metric is required", http.StatusBadRequest)
			return
		}
		node, err := repo.Node(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if node == nil {
			http.NotFound(w, r)
			return
		}
		history, err := repo.History(id, metric, r.URL.Query().Get("range"))
		writeJSON(w, history, err)
	})
	s.mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := repo.Stats(optionalInt(r.URL.Query().Get("task_id")))
		writeJSON(w, stats, err)
	})
	s.mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		nodes, err := repo.Nodes(optionalInt(r.URL.Query().Get("task_id")))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(BuildPrometheus(nodes)))
	})
	s.mux.HandleFunc("POST /api/tasks/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathInt(w, r, "id")
		if !ok {
			return
		}
		if s.opts.Runner == nil {
			writeError(w, http.StatusServiceUnavailable, fmt.Errorf("probe runner is not configured"))
			return
		}
		summary, err := s.opts.Runner.RunTask(id)
		writeJSON(w, summary, err)
	})
	s.mux.HandleFunc("POST /api/tests/run", func(w http.ResponseWriter, _ *http.Request) {
		if s.opts.Runner == nil {
			writeError(w, http.StatusServiceUnavailable, fmt.Errorf("probe runner is not configured"))
			return
		}
		summary, err := s.opts.Runner.RunAll()
		writeJSON(w, summary, err)
	})
	if s.opts.StaticDir != "" {
		assetsDir := filepath.Join(s.opts.StaticDir, "assets")
		s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir))))
		s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			indexPath := filepath.Join(s.opts.StaticDir, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
			writeJSONValue(w, map[string]string{
				"message": "Proxy Check API is running. Build frontend/ to enable the dashboard.",
				"api":     "/api",
			})
		})
	}
}

func writeJSON[T any](w http.ResponseWriter, value T, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONValue(w, value)
}

func writeJSONValue(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	http.Error(w, err.Error(), status)
}

func defaultOptions() Options {
	return Options{
		ConfigDir:         "./runtime/configs",
		StaticDir:         "",
		ListenerPortStart: 20000,
		ListenerPortMax:   65000,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func mergeOptions(base Options, override Options) Options {
	if override.ConfigDir != "" {
		base.ConfigDir = override.ConfigDir
	}
	if override.StaticDir != "" {
		base.StaticDir = override.StaticDir
	}
	if override.ListenerPortStart != 0 {
		base.ListenerPortStart = override.ListenerPortStart
	}
	if override.ListenerPortMax != 0 {
		base.ListenerPortMax = override.ListenerPortMax
	}
	if override.HTTPClient != nil {
		base.HTTPClient = override.HTTPClient
	}
	if override.Runner != nil {
		base.Runner = override.Runner
	}
	if override.AllowPrivateConfigURLs {
		base.AllowPrivateConfigURLs = true
	}
	return base
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) createImportedTask(name, sourceURL string, intervalSeconds int, enabled bool, advancedProbesEnabled bool) (*storage.Task, []storage.Node, error) {
	content, err := s.fetchConfig(sourceURL)
	if err != nil {
		return nil, nil, err
	}
	if _, err := clash.LoadNodes(content); err != nil {
		return nil, nil, err
	}
	task, err := s.repo.CreateTask(name, sourceURL, "", intervalSeconds, enabled, advancedProbesEnabled)
	if err != nil {
		return nil, nil, err
	}
	configPath, err := s.writeTaskConfig(task.ID, content)
	if err != nil {
		_ = s.repo.DeleteTask(task.ID)
		return nil, nil, err
	}
	task, nodes, err := s.syncTaskConfig(task.ID, configPath, content)
	if err != nil {
		_ = s.repo.DeleteTask(task.ID)
	}
	return task, nodes, err
}

func (s *Server) refreshTask(id int) (*storage.Task, []storage.Node, error) {
	task, err := s.repo.GetTask(id)
	if err != nil {
		return nil, nil, err
	}
	if task == nil {
		return nil, nil, fmt.Errorf("task not found")
	}
	content, err := s.fetchConfig(task.SourceURL)
	if err != nil {
		_ = s.recordTaskRefreshError(task.ID, err)
		return nil, nil, err
	}
	configPath, err := s.writeTaskConfig(task.ID, content)
	if err != nil {
		_ = s.recordTaskRefreshError(task.ID, err)
		return nil, nil, err
	}
	task, nodes, err := s.syncTaskConfig(task.ID, configPath, content)
	if err != nil {
		_ = s.recordTaskRefreshError(id, err)
	}
	return task, nodes, err
}

func (s *Server) syncTaskConfig(taskID int, configPath string, content []byte) (*storage.Task, []storage.Node, error) {
	clashNodes, err := clash.LoadNodes(content)
	if err != nil {
		return nil, nil, err
	}
	inputs := make([]storage.NodeInput, 0, len(clashNodes))
	for _, node := range clashNodes {
		inputs = append(inputs, storage.NodeInput{
			Name:      node.Name,
			Type:      node.Type,
			Server:    node.Server,
			Port:      node.Port,
			RawConfig: node.RawConfig,
		})
	}
	nodes, err := s.repo.SyncNodes(taskID, inputs, s.opts.ListenerPortStart, s.opts.ListenerPortMax)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	status := "unknown"
	task, err := s.repo.UpdateTask(taskID, storage.TaskPatch{
		ConfigPath:            &configPath,
		Status:                &status,
		LastRefreshAt:         &now,
		ClearLastRefreshError: true,
	})
	if err != nil {
		return nil, nil, err
	}
	return task, nodes, nil
}

func (s *Server) recordTaskRefreshError(taskID int, refreshErr error) error {
	message := refreshErr.Error()
	_, err := s.repo.UpdateTask(taskID, storage.TaskPatch{LastRefreshError: &message})
	return err
}

func (s *Server) fetchConfig(rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid config URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("only http/https Clash config URLs are supported")
	}
	if !s.opts.AllowPrivateConfigURLs {
		if err := rejectPrivateConfigURL(parsed); err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	var remoteAddr net.Addr
	if !s.opts.AllowPrivateConfigURLs {
		trace := &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				if info.Conn != nil {
					remoteAddr = info.Conn.RemoteAddr()
				}
			},
		}
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	}
	response, err := s.opts.HTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if !s.opts.AllowPrivateConfigURLs {
		if err := rejectPrivateRemoteAddr(remoteAddr); err != nil {
			return nil, err
		}
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, fmt.Errorf("redirects are not followed (status=%d); submit the final URL directly", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed with status %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if _, err := clash.LoadNodes(content); err != nil {
		return nil, err
	}
	return content, nil
}

func rejectPrivateConfigURL(parsed *url.URL) error {
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("config URL host is required")
	}
	lowerHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") {
		return fmt.Errorf("private config URLs are not allowed")
	}
	if addr, err := netip.ParseAddr(lowerHost); err == nil {
		if isUnsafeConfigAddr(addr) {
			return fmt.Errorf("private config URLs are not allowed")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve config URL host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve config URL host: no addresses")
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return fmt.Errorf("resolve config URL host: invalid address")
		}
		if isUnsafeConfigAddr(addr) {
			return fmt.Errorf("private config URLs are not allowed")
		}
	}
	return nil
}

func isUnsafeConfigAddr(addr netip.Addr) bool {
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified()
}

func rejectPrivateRemoteAddr(remote net.Addr) error {
	if remote == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err != nil {
		host = remote.String()
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	if isUnsafeConfigAddr(addr) {
		return fmt.Errorf("private config URLs are not allowed")
	}
	return nil
}

func (s *Server) writeTaskConfig(taskID int, content []byte) (string, error) {
	if err := os.MkdirAll(s.opts.ConfigDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(s.opts.ConfigDir, fmt.Sprintf("task-%d.yaml", taskID))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func optionalInt(value string) *int {
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func pathInt(w http.ResponseWriter, r *http.Request, key string) (int, bool) {
	value, err := strconv.Atoi(r.PathValue(key))
	if err != nil {
		http.Error(w, "invalid path id", http.StatusBadRequest)
		return 0, false
	}
	return value, true
}

func BuildPrometheus(nodes []storage.Node) string {
	lines := []string{
		"# HELP proxy_check_node_score Computed node quality score.",
		"# TYPE proxy_check_node_score gauge",
		"# HELP proxy_check_node_score_confidence Share of score weight backed by current data.",
		"# TYPE proxy_check_node_score_confidence gauge",
		"# HELP proxy_check_node_availability Node availability as 1 for available and 0 otherwise.",
		"# TYPE proxy_check_node_availability gauge",
		"# HELP proxy_check_node_metric_latency_ms Latest metric latency in milliseconds.",
		"# TYPE proxy_check_node_metric_latency_ms gauge",
		"# HELP proxy_check_node_metric_value Latest metric value.",
		"# TYPE proxy_check_node_metric_value gauge",
	}
	for _, node := range nodes {
		base := nodeLabels(node, "", "")
		if node.Score != nil {
			lines = append(lines, fmt.Sprintf("proxy_check_node_score{%s} %s", labels(base), storage.FormatFloat(*node.Score)))
		}
		lines = append(lines, fmt.Sprintf("proxy_check_node_score_confidence{%s} %s", labels(base), storage.FormatFloat(node.Confidence)))
		availability := 0
		if node.Status == "available" {
			availability = 1
		}
		lines = append(lines, fmt.Sprintf("proxy_check_node_availability{%s} %d", labels(base), availability))
		for _, summary := range node.Metrics {
			metricLabels := nodeLabels(node, summary.Metric, summary.Target)
			if summary.LatencyMS != nil {
				lines = append(lines, fmt.Sprintf("proxy_check_node_metric_latency_ms{%s} %s", labels(metricLabels), storage.FormatFloat(*summary.LatencyMS)))
			}
			if summary.Value != nil {
				lines = append(lines, fmt.Sprintf("proxy_check_node_metric_value{%s} %s", labels(metricLabels), storage.FormatFloat(*summary.Value)))
			}
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func nodeLabels(node storage.Node, metric, target string) map[string]string {
	taskID := ""
	if node.TaskID != nil {
		taskID = strconv.Itoa(*node.TaskID)
	}
	values := map[string]string{
		"node_id":   strconv.Itoa(node.ID),
		"node_name": node.Name,
		"task_id":   taskID,
		"status":    node.Status,
	}
	if metric != "" {
		values["metric"] = metric
	}
	if target != "" {
		values["target"] = target
	}
	return values
}

func labels(values map[string]string) string {
	order := []string{"node_id", "node_name", "task_id", "status", "metric", "target"}
	parts := make([]string, 0, len(values))
	for _, key := range order {
		value, ok := values[key]
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf(`%s="%s"`, key, escapeLabel(value)))
	}
	return strings.Join(parts, ",")
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}
