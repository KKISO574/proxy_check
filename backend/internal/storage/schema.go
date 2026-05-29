package storage

import "fmt"

func (r *Repository) EnsureSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS monitor_tasks (
			id INTEGER PRIMARY KEY,
			name VARCHAR(255),
			source_url TEXT,
			config_path TEXT,
			enabled BOOLEAN,
			interval_seconds INTEGER,
			advanced_probes_enabled BOOLEAN DEFAULT 0,
			status VARCHAR(32),
			last_refresh_at DATETIME,
			last_refresh_error TEXT,
			last_checked_at DATETIME,
			next_run_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS nodes (
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
		`CREATE TABLE IF NOT EXISTS probe_results (
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
		`CREATE TABLE IF NOT EXISTS node_meta (
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
		`CREATE INDEX IF NOT EXISTS ix_nodes_task_name ON nodes(task_id, name)`,
		`CREATE INDEX IF NOT EXISTS ix_probe_results_node_metric_id ON probe_results(node_id, metric, id)`,
		`CREATE INDEX IF NOT EXISTS ix_node_meta_node_id ON node_meta(node_id)`,
	}
	for _, stmt := range statements {
		if _, err := r.db.Exec(stmt); err != nil {
			return err
		}
	}

	migrations := map[string]map[string]string{
		"monitor_tasks": {
			"source_url":              "TEXT",
			"config_path":             "TEXT",
			"enabled":                 "BOOLEAN",
			"interval_seconds":        "INTEGER",
			"advanced_probes_enabled": "BOOLEAN DEFAULT 0",
			"status":                  "VARCHAR(32)",
			"last_refresh_at":         "DATETIME",
			"last_refresh_error":      "TEXT",
			"last_checked_at":         "DATETIME",
			"next_run_at":             "DATETIME",
			"created_at":              "DATETIME",
			"updated_at":              "DATETIME",
		},
		"nodes": {
			"task_id":         "INTEGER",
			"type":            "VARCHAR(64)",
			"server":          "VARCHAR(255)",
			"port":            "INTEGER",
			"raw_config":      "TEXT",
			"listener_port":   "INTEGER",
			"status":          "VARCHAR(32)",
			"last_checked_at": "DATETIME",
			"created_at":      "DATETIME",
			"updated_at":      "DATETIME",
		},
		"probe_results": {
			"target":     "VARCHAR(255)",
			"latency_ms": "FLOAT",
			"value":      "FLOAT",
			"data":       "TEXT",
			"success":    "BOOLEAN",
			"error":      "TEXT",
			"created_at": "DATETIME",
		},
	}
	for table, columns := range migrations {
		existing, err := r.tableColumns(table)
		if err != nil {
			return err
		}
		for column, ddl := range columns {
			if existing[column] {
				continue
			}
			if _, err := r.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, ddl)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) tableColumns(table string) (map[string]bool, error) {
	rows, err := r.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}
