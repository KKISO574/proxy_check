package probe

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"proxycheck/backend/internal/storage"
)

func TestServiceRunTaskSavesProbeResultsAndUpdatesTask(t *testing.T) {
	dbPath := seedServiceSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	service := NewService(repo, Options{
		Concurrency: 2,
		Probers: []Prober{
			StaticProber(func(node storage.Node) []storage.ProbeResultInput {
				if node.Name == "node-a" {
					return []storage.ProbeResultInput{
						{Metric: "delay", Target: "delay-url", LatencyMS: floatPtr(100), Value: floatPtr(100), Success: true},
						{Metric: "tcping", Target: "1.1.1.1:443", LatencyMS: floatPtr(80), Value: floatPtr(80), Success: true},
					}
				}
				return []storage.ProbeResultInput{
					{Metric: "delay", Target: "delay-url", Success: false, Error: stringPtr("timeout")},
					{Metric: "tcping", Target: "1.1.1.1:443", LatencyMS: floatPtr(90), Value: floatPtr(90), Success: true},
				}
			}),
		},
	})

	summary, err := service.RunTask(1)
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if summary.Nodes != 2 || summary.Results != 4 || summary.Errors != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open check db: %v", err)
	}
	defer db.Close()

	var resultCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM probe_results").Scan(&resultCount); err != nil {
		t.Fatalf("count results: %v", err)
	}
	if resultCount != 4 {
		t.Fatalf("expected 4 stored results, got %d", resultCount)
	}
	var taskStatus string
	var checked, nextRun any
	if err := db.QueryRow("SELECT status, last_checked_at, next_run_at FROM monitor_tasks WHERE id = 1").Scan(&taskStatus, &checked, &nextRun); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if taskStatus != "available" || checked == nil || nextRun == nil {
		t.Fatalf("expected available checked task, got status=%q checked=%#v next=%#v", taskStatus, checked, nextRun)
	}
}

func TestServiceRunAllRunsEnabledTasks(t *testing.T) {
	dbPath := seedServiceSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	service := NewService(repo, Options{
		Probers: []Prober{
			StaticProber(func(storage.Node) []storage.ProbeResultInput {
				return []storage.ProbeResultInput{
					{Metric: "delay", Target: "delay-url", LatencyMS: floatPtr(100), Value: floatPtr(100), Success: true},
				}
			}),
		},
	})

	summary, err := service.RunAll()
	if err != nil {
		t.Fatalf("run all: %v", err)
	}
	if summary.Nodes != 2 || summary.Results != 2 || summary.Errors != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestServiceRunTaskCallsBeforeTaskRunHook(t *testing.T) {
	dbPath := seedServiceSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	var hookTaskID int
	var hookNodeCount int
	service := NewService(repo, Options{
		Probers: []Prober{
			StaticProber(func(storage.Node) []storage.ProbeResultInput {
				return []storage.ProbeResultInput{{Metric: "delay", Target: "delay-url", Success: true}}
			}),
		},
		BeforeTaskRun: func(task *storage.Task, nodes []storage.Node) error {
			hookTaskID = task.ID
			hookNodeCount = len(nodes)
			return nil
		},
	})

	if _, err := service.RunTask(1); err != nil {
		t.Fatalf("run task: %v", err)
	}
	if hookTaskID != 1 || hookNodeCount != 2 {
		t.Fatalf("unexpected hook call task=%d nodes=%d", hookTaskID, hookNodeCount)
	}
}

func TestServiceRunTaskSkipsAdvancedProbersUnlessTaskEnablesThem(t *testing.T) {
	dbPath := seedServiceSQLite(t)
	repo, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	service := NewService(repo, Options{
		Probers: []Prober{
			StaticProber(func(storage.Node) []storage.ProbeResultInput {
				return []storage.ProbeResultInput{{Metric: "delay", Target: "delay-url", Success: true}}
			}),
			AdvancedStaticProber(func(storage.Node) []storage.ProbeResultInput {
				return []storage.ProbeResultInput{{Metric: "miaospeed_unlock", Target: "unlock", Success: true}}
			}),
		},
	})

	summary, err := service.RunTask(1)
	if err != nil {
		t.Fatalf("run task without advanced probes: %v", err)
	}
	if summary.Results != 2 {
		t.Fatalf("advanced prober should be skipped by default, summary=%#v", summary)
	}

	enabled := true
	if _, err := repo.UpdateTask(1, storage.TaskPatch{AdvancedProbesEnabled: &enabled}); err != nil {
		t.Fatalf("enable advanced probes: %v", err)
	}
	summary, err = service.RunTask(1)
	if err != nil {
		t.Fatalf("run task with advanced probes: %v", err)
	}
	if summary.Results != 4 {
		t.Fatalf("advanced prober should run when enabled, summary=%#v", summary)
	}
}

type StaticProber func(node storage.Node) []storage.ProbeResultInput

func (p StaticProber) Probe(_ context.Context, node storage.Node) []storage.ProbeResultInput {
	return p(node)
}

type AdvancedStaticProber func(node storage.Node) []storage.ProbeResultInput

func (p AdvancedStaticProber) Probe(_ context.Context, node storage.Node) []storage.ProbeResultInput {
	return p(node)
}

func (p AdvancedStaticProber) AdvancedProbe() bool {
	return true
}

func seedServiceSQLite(t *testing.T) string {
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
			VALUES
			(1, '任务一', 'local://one', '/tmp/one.yaml', 1, 60, 'unknown', 0, '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z'),
			(2, '任务二', 'local://two', '/tmp/two.yaml', 0, 60, 'unknown', 0, '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z')`,
		`INSERT INTO nodes (id, task_id, name, type, server, port, listener_port, status, created_at, updated_at)
			VALUES
			(1, 1, 'node-a', 'ss', 'example.com', 443, 20001, 'unknown', '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z'),
			(2, 1, 'node-b', 'trojan', 'example.net', 443, 20002, 'unknown', '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z'),
			(3, 2, 'disabled-node', 'ss', 'disabled.example.com', 443, 20003, 'unknown', '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z')`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed statement failed: %v\n%s", err, stmt)
		}
	}
	return dbPath
}

func floatPtr(value float64) *float64 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
