package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"proxycheck/backend/internal/api"
	"proxycheck/backend/internal/storage"
)

func TestGoAPIServesReadOnlyParityFromSQLite(t *testing.T) {
	dbPath := seedSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()
	server := api.NewServer(repo)

	tasksResponse := getJSON(t, server, "/api/tasks")
	tasks := tasksResponse.([]any)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0].(map[string]any)
	if task["name"] != "默认配置" {
		t.Fatalf("unexpected task name: %#v", task["name"])
	}
	if task["node_count"] != float64(2) {
		t.Fatalf("unexpected node_count: %#v", task["node_count"])
	}

	nodesResponse := getJSON(t, server, "/api/nodes?task_id=1")
	nodes := nodesResponse.([]any)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	node := nodes[0].(map[string]any)
	if node["name"] != "node-a" {
		t.Fatalf("unexpected first node: %#v", node["name"])
	}
	metrics := node["metrics"].(map[string]any)
	delay := metrics["delay"].(map[string]any)
	if delay["latency_ms"] != float64(120) {
		t.Fatalf("unexpected delay latency: %#v", delay["latency_ms"])
	}
	packetLoss := metrics["packet_loss"].(map[string]any)
	if packetLoss["value"] != float64(5) {
		t.Fatalf("unexpected packet loss value: %#v", packetLoss["value"])
	}
	meta := node["meta"].(map[string]any)
	if meta["country"] != "US" || meta["asn"] != "AS64500" {
		t.Fatalf("unexpected meta: %#v", meta)
	}
	if node["score"] == nil {
		t.Fatalf("expected score to be present")
	}
	breakdown := node["score_breakdown"].(map[string]any)
	if _, ok := breakdown["delay"]; !ok {
		t.Fatalf("expected delay score breakdown: %#v", breakdown)
	}

	statsResponse := getJSON(t, server, "/api/stats?task_id=1").(map[string]any)
	if statsResponse["total_nodes"] != float64(2) {
		t.Fatalf("unexpected total_nodes: %#v", statsResponse["total_nodes"])
	}
	if statsResponse["available_nodes"] != float64(1) {
		t.Fatalf("unexpected available_nodes: %#v", statsResponse["available_nodes"])
	}
	if statsResponse["average_delay_ms"] != float64(120) {
		t.Fatalf("unexpected average_delay_ms: %#v", statsResponse["average_delay_ms"])
	}

	detail := getJSON(t, server, "/api/nodes/2").(map[string]any)
	errors := detail["recent_errors"].([]any)
	if len(errors) != 1 {
		t.Fatalf("expected one recent error, got %#v", errors)
	}

	prom := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics?task_id=1", nil)
	server.ServeHTTP(prom, req)
	if prom.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", prom.Code)
	}
	body := prom.Body.String()
	for _, want := range []string{
		"# TYPE proxy_check_node_score gauge",
		`proxy_check_node_availability{node_id="1",node_name="node-a",task_id="1",status="available"} 1`,
		`proxy_check_node_metric_latency_ms{node_id="1",node_name="node-a",task_id="1",status="available",metric="delay",target="https://cp.cloudflare.com/generate_204"} 120`,
		`proxy_check_node_metric_value{node_id="1",node_name="node-a",task_id="1",status="available",metric="packet_loss",target="1.1.1.1:443"} 5`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
}

func TestGoAPITaskImportLifecycle(t *testing.T) {
	dbPath := seedSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	configContent := `proxies:
  - name: imported-a
    type: ss
    server: imported.example.com
    port: 443
    cipher: aes-128-gcm
    password: secret-a
  - name: imported-a
    type: ss
    server: duplicate.example.com
    port: 444
  - name: imported-b
    type: trojan
    server: imported-b.example.com
    port: 8443
    password: secret-b
`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(configContent))
	}))
	defer upstream.Close()

	configDir := t.TempDir()
	server := api.NewServer(
		repo,
		api.Options{
			ConfigDir:              configDir,
			ListenerPortStart:      30000,
			ListenerPortMax:        30010,
			HTTPClient:             upstream.Client(),
			AllowPrivateConfigURLs: true,
		},
	)

	created := postJSON(t, server, "/api/tasks", map[string]any{
		"name":                    "远程配置",
		"source_url":              upstream.URL,
		"interval_seconds":        90,
		"enabled":                 true,
		"advanced_probes_enabled": true,
	}).(map[string]any)
	if created["nodes"] != float64(2) {
		t.Fatalf("expected 2 deduped nodes, got %#v", created["nodes"])
	}
	task := created["task"].(map[string]any)
	taskID := int(task["id"].(float64))
	if task["node_count"] != float64(2) {
		t.Fatalf("unexpected node_count: %#v", task["node_count"])
	}
	if task["last_refresh_error"] != nil {
		t.Fatalf("unexpected refresh error: %#v", task["last_refresh_error"])
	}
	if task["advanced_probes_enabled"] != true {
		t.Fatalf("expected advanced probes enabled on created task, got %#v", task)
	}
	if _, err := os.Stat(filepath.Join(configDir, "task-2.yaml")); err != nil {
		t.Fatalf("expected imported config file: %v", err)
	}

	nodes := getJSON(t, server, "/api/nodes?task_id=2").([]any)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 imported nodes, got %d", len(nodes))
	}
	firstNode := nodes[0].(map[string]any)
	if firstNode["name"] != "imported-a" || firstNode["listener_port"] != float64(30000) {
		t.Fatalf("unexpected first imported node: %#v", firstNode)
	}
	raw := queryString(t, dbPath, "SELECT raw_config FROM nodes WHERE task_id = 2 AND name = 'imported-a'")
	if !strings.Contains(raw, "password: secret-a") || strings.Contains(raw, "duplicate.example.com") {
		t.Fatalf("raw_config was not deduped/persisted correctly:\n%s", raw)
	}

	updated := patchJSON(t, server, "/api/tasks/2", map[string]any{
		"name":                    "已暂停配置",
		"enabled":                 false,
		"interval_seconds":        120,
		"advanced_probes_enabled": false,
	}).(map[string]any)
	updatedTask := updated["task"].(map[string]any)
	if updatedTask["name"] != "已暂停配置" || updatedTask["enabled"] != false || updatedTask["interval_seconds"] != float64(120) || updatedTask["advanced_probes_enabled"] != false {
		t.Fatalf("unexpected updated task: %#v", updatedTask)
	}

	configContent = `proxies:
  - name: imported-a
    type: ss
    server: imported-new.example.com
    port: 443
`
	refreshed := postJSON(t, server, "/api/tasks/2/refresh", map[string]any{}).(map[string]any)
	if refreshed["nodes"] != float64(1) {
		t.Fatalf("expected 1 refreshed node, got %#v", refreshed["nodes"])
	}
	tasks := getJSON(t, server, "/api/tasks").([]any)
	refreshedTask := tasks[1].(map[string]any)
	if refreshedTask["node_count"] != float64(1) {
		t.Fatalf("expected removed nodes excluded from count, got %#v", refreshedTask["node_count"])
	}

	deleteRequest(t, server, "/api/tasks/"+strconv.Itoa(taskID))
	tasksAfterDelete := getJSON(t, server, "/api/tasks").([]any)
	if len(tasksAfterDelete) != 1 {
		t.Fatalf("expected only seed task after delete, got %d", len(tasksAfterDelete))
	}
	nodesAfterDelete := getJSON(t, server, "/api/nodes?task_id=2").([]any)
	if len(nodesAfterDelete) != 0 {
		t.Fatalf("expected deleted task nodes removed, got %#v", nodesAfterDelete)
	}
}

func TestGoAPIRunEndpointsUseProbeRunner(t *testing.T) {
	dbPath := seedSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	runner := &fakeRunner{
		taskSummary: api.RunSummary{Nodes: 2, Results: 8, Errors: 1},
		allSummary:  api.RunSummary{Nodes: 3, Results: 12, Errors: 2},
	}
	server := api.NewServer(repo, api.Options{Runner: runner})

	taskRun := postJSON(t, server, "/api/tasks/1/run", map[string]any{}).(map[string]any)
	if taskRun["nodes"] != float64(2) || taskRun["results"] != float64(8) || taskRun["errors"] != float64(1) {
		t.Fatalf("unexpected task run summary: %#v", taskRun)
	}
	if runner.taskID != 1 {
		t.Fatalf("expected runner to receive task id 1, got %d", runner.taskID)
	}

	allRun := postJSON(t, server, "/api/tests/run", map[string]any{}).(map[string]any)
	if allRun["nodes"] != float64(3) || allRun["results"] != float64(12) || allRun["errors"] != float64(2) {
		t.Fatalf("unexpected global run summary: %#v", allRun)
	}
	if runner.runAllCalls != 1 {
		t.Fatalf("expected one global run, got %d", runner.runAllCalls)
	}
}

func TestGoAPIMiaoSpeedEndpointsExposeCatalogRunAndResults(t *testing.T) {
	dbPath := seedSQLite(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	_, err = db.Exec(`
		UPDATE node_meta SET dns_leak = 'clean' WHERE node_id = 1;
		INSERT INTO probe_results (id, node_id, metric, target, latency_ms, value, data, success, error, created_at)
		VALUES
			(5, 1, 'miaospeed_full', 'full', NULL, 88.5, '{"download_mbps":88.5,"upload_mbps":12.25,"http_code":"204","packet_loss":0,"services":{"netflix":"US","openai":"允许"}}', 1, NULL, '2026-05-28T00:02:10Z'),
			(6, 2, 'miaospeed_full', 'full', NULL, 25, '{"download_mbps":25,"upload_mbps":3.5,"http_code":"200","packet_loss":7.5,"services":{"netflix":"失败"}}', 1, NULL, '2026-05-28T00:02:11Z')
	`)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close seed db: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("seed miaospeed results: %v", err)
	}
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	runner := &fakeRunner{
		advancedSummary: api.RunSummary{Nodes: 2, Results: 2, Errors: 0},
	}
	server := api.NewServer(repo, api.Options{Runner: runner})

	catalog := getJSON(t, server, "/api/miaospeed/catalog").([]any)
	if len(catalog) == 0 {
		t.Fatalf("expected non-empty miaospeed catalog")
	}
	foundOpenAI := false
	for _, item := range catalog {
		service := item.(map[string]any)
		if service["key"] == "openai" && service["script_id"] == "openai_unlock" {
			foundOpenAI = true
		}
	}
	if !foundOpenAI {
		t.Fatalf("expected openai service in catalog: %#v", catalog)
	}

	run := postJSON(t, server, "/api/tasks/1/miaospeed/run", map[string]any{}).(map[string]any)
	if run["nodes"] != float64(2) || run["results"] != float64(2) || run["errors"] != float64(0) {
		t.Fatalf("unexpected advanced run summary: %#v", run)
	}
	if runner.advancedTaskID != 1 || runner.advancedCalls != 1 {
		t.Fatalf("expected one advanced run for task 1, got id=%d calls=%d", runner.advancedTaskID, runner.advancedCalls)
	}

	grid := getJSON(t, server, "/api/tasks/1/miaospeed/results").(map[string]any)
	rows := grid["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("expected two miaospeed rows, got %#v", rows)
	}
	first := rows[0].(map[string]any)
	if first["node_name"] != "node-a" || first["download_mbps"] != float64(88.5) || first["upload_mbps"] != float64(12.25) || first["http_code"] != "204" || first["dns_leak"] != "clean" {
		t.Fatalf("unexpected first miaospeed row: %#v", first)
	}
	firstServices := first["services"].(map[string]any)
	if firstServices["netflix"] != "US" || firstServices["openai"] != "允许" {
		t.Fatalf("unexpected first services: %#v", firstServices)
	}
	second := rows[1].(map[string]any)
	if second["node_name"] != "node-b" || second["packet_loss"] != float64(7.5) || second["dns_leak"] != nil {
		t.Fatalf("unexpected second miaospeed row: %#v", second)
	}
}

func TestGoAPIHistoryReturnsNotFoundForMissingNode(t *testing.T) {
	dbPath := seedSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()
	server := api.NewServer(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/999/history?metric=delay&range=1h", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing node history, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoAPIRejectsPrivateConfigImportURL(t *testing.T) {
	dbPath := seedSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: local-node
    type: ss
    server: local.example.com
    port: 443
`))
	}))
	defer upstream.Close()

	server := api.NewServer(repo, api.Options{HTTPClient: upstream.Client()})
	rec := postJSONStatus(t, server, "/api/tasks", map[string]any{
		"name":             "本地地址",
		"source_url":       upstream.URL,
		"interval_seconds": 60,
	}, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("expected private network rejection, got %s", rec.Body.String())
	}
}

func TestGoAPIRefreshFailureRecordsTaskError(t *testing.T) {
	dbPath := seedSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	configContent := `proxies:
  - name: imported-a
    type: ss
    server: imported.example.com
    port: 443
`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(configContent))
	}))
	defer upstream.Close()
	server := api.NewServer(repo, api.Options{
		ConfigDir:              t.TempDir(),
		ListenerPortStart:      30000,
		ListenerPortMax:        30010,
		HTTPClient:             upstream.Client(),
		AllowPrivateConfigURLs: true,
	})

	_ = postJSON(t, server, "/api/tasks", map[string]any{
		"name":             "远程配置",
		"source_url":       upstream.URL,
		"interval_seconds": 60,
	})
	configContent = `not: proxies`

	rec := postJSONStatus(t, server, "/api/tasks/2/refresh", map[string]any{}, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "proxies") {
		t.Fatalf("expected YAML error, got %s", rec.Body.String())
	}
	tasks := getJSON(t, server, "/api/tasks").([]any)
	task := tasks[1].(map[string]any)
	if task["last_refresh_error"] == nil || !strings.Contains(task["last_refresh_error"].(string), "proxies") {
		t.Fatalf("expected last_refresh_error to be recorded, got %#v", task)
	}
}

func TestGoAPICreateTaskCleansUpWhenConfigWriteFails(t *testing.T) {
	dbPath := seedSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: imported-a
    type: ss
    server: imported.example.com
    port: 443
`))
	}))
	defer upstream.Close()
	blockingFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	server := api.NewServer(repo, api.Options{
		ConfigDir:              blockingFile,
		HTTPClient:             upstream.Client(),
		AllowPrivateConfigURLs: true,
	})

	_ = postJSONStatus(t, server, "/api/tasks", map[string]any{
		"name":             "写入失败",
		"source_url":       upstream.URL,
		"interval_seconds": 60,
	}, http.StatusBadRequest)
	tasks := getJSON(t, server, "/api/tasks").([]any)
	if len(tasks) != 1 {
		t.Fatalf("failed create should clean up partial task, got %#v", tasks)
	}
}

type fakeRunner struct {
	taskID          int
	runAllCalls     int
	advancedTaskID  int
	advancedCalls   int
	taskSummary     api.RunSummary
	allSummary      api.RunSummary
	advancedSummary api.RunSummary
}

func (r *fakeRunner) RunTask(taskID int) (api.RunSummary, error) {
	r.taskID = taskID
	return r.taskSummary, nil
}

func (r *fakeRunner) RunAll() (api.RunSummary, error) {
	r.runAllCalls++
	return r.allSummary, nil
}

func (r *fakeRunner) RunAdvancedTask(taskID int) (api.RunSummary, error) {
	r.advancedTaskID = taskID
	r.advancedCalls++
	return r.advancedSummary, nil
}

func getJSON(t *testing.T, handler http.Handler, target string) any {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, body=%s", target, rec.Code, rec.Body.String())
	}
	var payload any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s: %v", target, err)
	}
	return payload
}

func postJSON(t *testing.T, handler http.Handler, target string, payload map[string]any) any {
	t.Helper()
	return requestJSON(t, handler, http.MethodPost, target, payload, http.StatusOK)
}

func patchJSON(t *testing.T, handler http.Handler, target string, payload map[string]any) any {
	t.Helper()
	return requestJSON(t, handler, http.MethodPatch, target, payload, http.StatusOK)
}

func requestJSON(t *testing.T, handler http.Handler, method string, target string, payload map[string]any, wantStatus int) any {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body=%s", method, target, rec.Code, wantStatus, rec.Body.String())
	}
	var decoded any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %s %s: %v", method, target, err)
	}
	return decoded
}

func postJSONStatus(t *testing.T, handler http.Handler, target string, payload map[string]any, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("POST %s status = %d, want %d, body=%s", target, rec.Code, wantStatus, rec.Body.String())
	}
	return rec
}

func deleteRequest(t *testing.T, handler http.Handler, target string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, target, nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE %s status = %d, body=%s", target, rec.Code, rec.Body.String())
	}
}

func queryString(t *testing.T, dbPath string, query string) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open query db: %v", err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("query string: %v", err)
	}
	return value
}

func seedSQLite(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "proxy_check.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE monitor_tasks (
			id INTEGER PRIMARY KEY,
			name VARCHAR(255),
			source_url TEXT,
			config_path TEXT,
			enabled BOOLEAN,
			interval_seconds INTEGER,
			status VARCHAR(32),
			advanced_probes_enabled BOOLEAN,
			last_refresh_at DATETIME,
			last_refresh_error TEXT,
			last_checked_at DATETIME,
			next_run_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE nodes (
			id INTEGER PRIMARY KEY,
			task_id INTEGER,
			name VARCHAR(255),
			type VARCHAR(64),
			server VARCHAR(255),
			port INTEGER,
			raw_config TEXT,
			listener_port INTEGER,
			status VARCHAR(32),
			last_checked_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE probe_results (
			id INTEGER PRIMARY KEY,
			node_id INTEGER,
			metric VARCHAR(32),
			target VARCHAR(255),
			latency_ms FLOAT,
			value FLOAT,
			data TEXT,
			success BOOLEAN,
			error TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE node_meta (
			id INTEGER PRIMARY KEY,
			node_id INTEGER,
			exit_ip VARCHAR(64),
			asn VARCHAR(64),
			country VARCHAR(64),
			region VARCHAR(128),
			isp VARCHAR(255),
			netflix_unlock VARCHAR(64),
			disney_unlock VARCHAR(64),
			openai_unlock VARCHAR(64),
			youtube_unlock VARCHAR(64),
			dns_leak VARCHAR(64),
			updated_at DATETIME
		)`,
		`INSERT INTO monitor_tasks (id, name, source_url, config_path, enabled, interval_seconds, status, advanced_probes_enabled, created_at, updated_at)
			VALUES (1, '默认配置', 'local://test', '/tmp/task.yaml', 1, 60, 'available', 0, '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z')`,
		`INSERT INTO nodes (id, task_id, name, type, server, port, listener_port, status, last_checked_at, created_at, updated_at)
			VALUES
			(1, 1, 'node-a', 'ss', 'example.com', 443, 20001, 'available', '2026-05-28T00:01:00Z', '2026-05-28T00:00:00Z', '2026-05-28T00:01:00Z'),
			(2, 1, 'node-b', 'trojan', 'example.net', 443, 20002, 'down', '2026-05-28T00:01:00Z', '2026-05-28T00:00:00Z', '2026-05-28T00:01:00Z')`,
		`INSERT INTO probe_results (id, node_id, metric, target, latency_ms, value, data, success, error, created_at)
			VALUES
			(1, 1, 'delay', 'https://cp.cloudflare.com/generate_204', 150, 150, NULL, 1, NULL, '2026-05-28T00:00:10Z'),
			(2, 1, 'delay', 'https://cp.cloudflare.com/generate_204', 120, 120, NULL, 1, NULL, '2026-05-28T00:01:10Z'),
			(3, 1, 'packet_loss', '1.1.1.1:443', NULL, 5, '{"sent":20,"failed":1}', 1, NULL, '2026-05-28T00:01:12Z'),
			(4, 2, 'delay', 'https://cp.cloudflare.com/generate_204', NULL, NULL, NULL, 0, 'timeout', '2026-05-28T00:01:11Z')`,
		`INSERT INTO node_meta (id, node_id, exit_ip, asn, country, region, isp, updated_at)
			VALUES (1, 1, '203.0.113.10', 'AS64500', 'US', 'California', 'Example ISP', '2026-05-28T00:01:00Z')`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed statement failed: %v\n%s", err, stmt)
		}
	}
	return dbPath
}
