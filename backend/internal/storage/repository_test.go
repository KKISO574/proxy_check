package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRepositorySaveProbeBatchUpdatesResultsAndNodeStatus(t *testing.T) {
	dbPath := seedRepositorySQLite(t)
	repo, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	if err := repo.SaveProbeBatch(1, []ProbeResultInput{
		{
			Metric:    "delay",
			Target:    "https://cp.cloudflare.com/generate_204",
			LatencyMS: floatPtrValue(125),
			Value:     floatPtrValue(125),
			Success:   true,
		},
		{
			Metric:  "tcping",
			Target:  "1.1.1.1:443",
			Success: false,
			Error:   stringPtrValue("timeout"),
		},
	}); err != nil {
		t.Fatalf("save probe batch: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open check db: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM probe_results WHERE node_id = 1").Scan(&count); err != nil {
		t.Fatalf("count results: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 probe rows, got %d", count)
	}
	var status string
	var checked any
	if err := db.QueryRow("SELECT status, last_checked_at FROM nodes WHERE id = 1").Scan(&status, &checked); err != nil {
		t.Fatalf("read node status: %v", err)
	}
	if status != "available" || checked == nil {
		t.Fatalf("expected available checked node, got status=%q checked=%#v", status, checked)
	}
}

func TestRepositoryEnsureSchemaCreatesTablesForFreshDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fresh.sqlite3")
	repo, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	if err := repo.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	task, err := repo.CreateTask("任务", "http://example.test/config.yaml", "/tmp/task.yaml", 60, true, false)
	if err != nil {
		t.Fatalf("create task after schema init: %v", err)
	}
	if task.AdvancedProbesEnabled {
		t.Fatalf("advanced probes should default to false")
	}
	if _, err := repo.Tasks(); err != nil {
		t.Fatalf("list tasks after schema init: %v", err)
	}
}

func TestRepositoryTaskAdvancedProbeFlagCanBePatched(t *testing.T) {
	dbPath := seedRepositorySQLite(t)
	repo, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	task, err := repo.GetTask(1)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.AdvancedProbesEnabled {
		t.Fatalf("seed task should not enable advanced probes")
	}
	enabled := true
	updated, err := repo.UpdateTask(1, TaskPatch{AdvancedProbesEnabled: &enabled})
	if err != nil {
		t.Fatalf("patch task: %v", err)
	}
	if !updated.AdvancedProbesEnabled {
		t.Fatalf("advanced probes flag was not patched: %#v", updated)
	}
}

func TestRepositoryDelaySamplesReturnsLatestSuccessfulDelayValues(t *testing.T) {
	dbPath := seedRepositorySQLite(t)
	repo, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()
	if err := repo.SaveProbeBatch(1, []ProbeResultInput{
		{Metric: "delay", Target: "delay-url", LatencyMS: floatPtrValue(100), Value: floatPtrValue(100), Success: true},
		{Metric: "delay", Target: "delay-url", Success: false, Error: stringPtrValue("timeout")},
		{Metric: "delay", Target: "delay-url", LatencyMS: floatPtrValue(120), Value: floatPtrValue(120), Success: true},
	}); err != nil {
		t.Fatalf("save delay batch: %v", err)
	}
	values, err := repo.DelaySamples(1, 2)
	if err != nil {
		t.Fatalf("delay samples: %v", err)
	}
	if len(values) != 2 || values[0] != 100 || values[1] != 120 {
		t.Fatalf("unexpected samples: %#v", values)
	}
}

func TestRepositoryUpsertNodeMetaCreatesAndUpdatesMetadata(t *testing.T) {
	dbPath := seedRepositorySQLite(t)
	repo, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()
	country := "US"
	asn := "AS64500"
	if err := repo.UpsertNodeMeta(1, NodeMeta{Country: &country, ASN: &asn}); err != nil {
		t.Fatalf("upsert meta: %v", err)
	}
	country = "JP"
	if err := repo.UpsertNodeMeta(1, NodeMeta{Country: &country, ASN: &asn}); err != nil {
		t.Fatalf("update meta: %v", err)
	}

	node, err := repo.Node(1)
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if node.Meta == nil || node.Meta.Country == nil || *node.Meta.Country != "JP" {
		t.Fatalf("unexpected node meta: %#v", node.Meta)
	}
}

func TestRepositoryUpsertNodeMetaPreservesExistingFieldsWhenPatchOmitsThem(t *testing.T) {
	dbPath := seedRepositorySQLite(t)
	repo, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repo.Close()

	exitIP := "203.0.113.10"
	asn := "AS64500"
	country := "US"
	if err := repo.UpsertNodeMeta(1, NodeMeta{ExitIP: &exitIP, ASN: &asn, Country: &country}); err != nil {
		t.Fatalf("upsert geo meta: %v", err)
	}
	dnsLeak := "clean"
	if err := repo.UpsertNodeMeta(1, NodeMeta{DNSLeak: &dnsLeak}); err != nil {
		t.Fatalf("upsert dns meta: %v", err)
	}

	node, err := repo.Node(1)
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if node.Meta == nil {
		t.Fatalf("expected node meta")
	}
	if node.Meta.ExitIP == nil || *node.Meta.ExitIP != exitIP || node.Meta.ASN == nil || *node.Meta.ASN != asn {
		t.Fatalf("geo fields should be preserved, got %#v", node.Meta)
	}
	if node.Meta.DNSLeak == nil || *node.Meta.DNSLeak != dnsLeak {
		t.Fatalf("dns leak should be updated, got %#v", node.Meta)
	}
}

func seedRepositorySQLite(t *testing.T) string {
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
			VALUES (1, '默认配置', 'local://test', '/tmp/task.yaml', 1, 60, 'unknown', 0, '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z')`,
		`INSERT INTO nodes (id, task_id, name, type, server, port, listener_port, status, created_at, updated_at)
			VALUES (1, 1, 'node-a', 'ss', 'example.com', 443, 20001, 'unknown', '2026-05-28T00:00:00Z', '2026-05-28T00:00:00Z')`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed statement failed: %v\n%s", err, stmt)
		}
	}
	return dbPath
}

func floatPtrValue(value float64) *float64 {
	return &value
}

func stringPtrValue(value string) *string {
	return &value
}
