package storage

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Repository struct {
	db *sql.DB
}

func OpenSQLite(path string) (*Repository, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) Tasks() ([]Task, error) {
	counts, err := r.taskNodeCounts()
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(`
		SELECT id, name, source_url, enabled, interval_seconds, advanced_probes_enabled, status,
		       config_path, last_refresh_at, last_refresh_error, last_checked_at, next_run_at
		FROM monitor_tasks
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var enabled, advancedProbesEnabled sql.NullBool
		var lastRefreshAt, lastRefreshError, lastCheckedAt, nextRunAt sql.NullString
		if err := rows.Scan(
			&task.ID,
			&task.Name,
			&task.SourceURL,
			&enabled,
			&task.IntervalSeconds,
			&advancedProbesEnabled,
			&task.Status,
			&task.ConfigPath,
			&lastRefreshAt,
			&lastRefreshError,
			&lastCheckedAt,
			&nextRunAt,
		); err != nil {
			return nil, err
		}
		task.Enabled = enabled.Bool
		task.AdvancedProbesEnabled = advancedProbesEnabled.Bool
		task.NodeCount = counts[task.ID]
		task.LastRefreshAt = stringPtr(lastRefreshAt)
		task.LastRefreshError = stringPtr(lastRefreshError)
		task.LastCheckedAt = stringPtr(lastCheckedAt)
		task.NextRunAt = stringPtr(nextRunAt)
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (r *Repository) GetTask(id int) (*Task, error) {
	counts, err := r.taskNodeCounts()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRow(`
		SELECT id, name, source_url, enabled, interval_seconds, advanced_probes_enabled, status,
		       config_path, last_refresh_at, last_refresh_error, last_checked_at, next_run_at
		FROM monitor_tasks
		WHERE id = ?
	`, id)
	task, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	task.NodeCount = counts[task.ID]
	return &task, nil
}

func (r *Repository) CreateTask(name, sourceURL, configPath string, intervalSeconds int, enabled bool, advancedProbesEnabled bool) (*Task, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := r.db.Exec(`
		INSERT INTO monitor_tasks
			(name, source_url, config_path, enabled, interval_seconds, advanced_probes_enabled, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'unknown', ?, ?)
	`, name, sourceURL, configPath, enabled, intervalSeconds, advancedProbesEnabled, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.GetTask(int(id))
}

func (r *Repository) UpdateTask(id int, patch TaskPatch) (*Task, error) {
	task, err := r.GetTask(id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}
	name := task.Name
	sourceURL := task.SourceURL
	configPath := task.ConfigPath
	enabled := task.Enabled
	intervalSeconds := task.IntervalSeconds
	advancedProbesEnabled := task.AdvancedProbesEnabled
	status := task.Status
	var lastRefreshAt, lastRefreshError, lastCheckedAt, nextRunAt any
	lastRefreshAt = task.LastRefreshAt
	lastRefreshError = task.LastRefreshError
	lastCheckedAt = task.LastCheckedAt
	nextRunAt = task.NextRunAt
	if patch.Name != nil {
		name = *patch.Name
	}
	if patch.SourceURL != nil {
		sourceURL = *patch.SourceURL
	}
	if patch.ConfigPath != nil {
		configPath = *patch.ConfigPath
	}
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	if patch.IntervalSeconds != nil {
		intervalSeconds = *patch.IntervalSeconds
	}
	if patch.AdvancedProbesEnabled != nil {
		advancedProbesEnabled = *patch.AdvancedProbesEnabled
	}
	if patch.Status != nil {
		status = *patch.Status
	}
	if patch.LastRefreshAt != nil {
		lastRefreshAt = *patch.LastRefreshAt
	}
	if patch.ClearLastRefreshError {
		lastRefreshError = nil
	}
	if patch.LastRefreshError != nil {
		lastRefreshError = *patch.LastRefreshError
	}
	if patch.LastCheckedAt != nil {
		lastCheckedAt = *patch.LastCheckedAt
	}
	if patch.NextRunAt != nil {
		nextRunAt = *patch.NextRunAt
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = r.db.Exec(`
		UPDATE monitor_tasks
		SET name = ?, source_url = ?, config_path = ?, enabled = ?,
		    interval_seconds = ?, advanced_probes_enabled = ?, status = ?, last_refresh_at = ?,
		    last_refresh_error = ?, last_checked_at = ?, next_run_at = ?,
		    updated_at = ?
		WHERE id = ?
	`, name, sourceURL, configPath, enabled, intervalSeconds, advancedProbesEnabled, status, nullableString(lastRefreshAt), nullableString(lastRefreshError), nullableString(lastCheckedAt), nullableString(nextRunAt), now, id)
	if err != nil {
		return nil, err
	}
	return r.GetTask(id)
}

func (r *Repository) DeleteTask(id int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		"DELETE FROM probe_results WHERE node_id IN (SELECT id FROM nodes WHERE task_id = ?)",
		"DELETE FROM node_meta WHERE node_id IN (SELECT id FROM nodes WHERE task_id = ?)",
		"DELETE FROM nodes WHERE task_id = ?",
		"DELETE FROM monitor_tasks WHERE id = ?",
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) Nodes(taskID *int) ([]Node, error) {
	query := `
		SELECT id, task_id, name, type, server, port, raw_config, listener_port, status, last_checked_at
		FROM nodes
	`
	args := []any{}
	if taskID != nil {
		query += " WHERE task_id = ?"
		args = append(args, *taskID)
	}
	query += " ORDER BY name ASC"
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := make([]Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range nodes {
		if err := r.fillNode(&nodes[i]); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

func (r *Repository) SyncNodes(taskID int, inputs []NodeInput, portStart, portMax int) ([]Node, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	existingRows, err := tx.Query("SELECT id, name, listener_port FROM nodes WHERE task_id = ?", taskID)
	if err != nil {
		return nil, err
	}
	existing := map[string]struct {
		id   int
		port *int
	}{}
	for existingRows.Next() {
		var id int
		var name string
		var listenerPort sql.NullInt64
		if err := existingRows.Scan(&id, &name, &listenerPort); err != nil {
			_ = existingRows.Close()
			return nil, err
		}
		existing[name] = struct {
			id   int
			port *int
		}{id: id, port: intPtr(listenerPort)}
	}
	if err := existingRows.Close(); err != nil {
		return nil, err
	}

	used, err := tx.Query("SELECT listener_port FROM nodes WHERE listener_port IS NOT NULL")
	if err != nil {
		return nil, err
	}
	usedPorts := map[int]struct{}{}
	for used.Next() {
		var port int
		if err := used.Scan(&port); err != nil {
			_ = used.Close()
			return nil, err
		}
		usedPorts[port] = struct{}{}
	}
	if err := used.Close(); err != nil {
		return nil, err
	}

	desired := map[string]struct{}{}
	outputIDs := make([]int, 0, len(inputs))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, input := range inputs {
		desired[input.Name] = struct{}{}
		current, exists := existing[input.Name]
		listenerPort := current.port
		if listenerPort == nil {
			port, err := nextFreePort(usedPorts, portStart, portMax)
			if err != nil {
				return nil, err
			}
			listenerPort = &port
			usedPorts[port] = struct{}{}
		}
		if exists {
			_, err = tx.Exec(`
				UPDATE nodes
				SET type = ?, server = ?, port = ?, raw_config = ?,
				    listener_port = ?,
				    status = CASE WHEN status = 'removed' THEN 'unknown' ELSE status END,
				    updated_at = ?
				WHERE id = ?
			`, nullableString(input.Type), nullableString(input.Server), nullableInt(input.Port), input.RawConfig, *listenerPort, now, current.id)
			if err != nil {
				return nil, err
			}
			outputIDs = append(outputIDs, current.id)
			continue
		}
		result, err := tx.Exec(`
			INSERT INTO nodes
				(task_id, name, type, server, port, raw_config, listener_port, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'unknown', ?, ?)
		`, taskID, input.Name, nullableString(input.Type), nullableString(input.Server), nullableInt(input.Port), input.RawConfig, *listenerPort, now, now)
		if err != nil {
			return nil, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		outputIDs = append(outputIDs, int(id))
	}

	for name, item := range existing {
		if _, ok := desired[name]; ok {
			continue
		}
		if _, err := tx.Exec("UPDATE nodes SET status = 'removed', updated_at = ? WHERE id = ?", now, item.id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	nodes := make([]Node, 0, len(outputIDs))
	for _, id := range outputIDs {
		node, err := r.Node(id)
		if err != nil {
			return nil, err
		}
		if node != nil {
			nodes = append(nodes, *node)
		}
	}
	return nodes, nil
}

func (r *Repository) Node(id int) (*Node, error) {
	row := r.db.QueryRow(`
		SELECT id, task_id, name, type, server, port, raw_config, listener_port, status, last_checked_at
		FROM nodes
		WHERE id = ?
	`, id)
	node, err := scanNode(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if err := r.fillNode(&node); err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *Repository) Stats(taskID *int) (Stats, error) {
	nodes, err := r.Nodes(taskID)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{TotalNodes: len(nodes)}
	var delaySum float64
	var delayCount int
	for _, node := range nodes {
		switch node.Status {
		case "available":
			stats.AvailableNodes++
		case "down":
			stats.DownNodes++
		}
		if delay, ok := node.Metrics["delay"]; ok && delay.Success && delay.LatencyMS != nil {
			delaySum += *delay.LatencyMS
			delayCount++
		}
	}
	stats.UnknownNodes = stats.TotalNodes - stats.AvailableNodes - stats.DownNodes
	if delayCount > 0 {
		avg := delaySum / float64(delayCount)
		stats.AverageDelayMS = &avg
	}
	return stats, nil
}

func (r *Repository) History(nodeID int, metric string, rangeName string) ([]MetricSummary, error) {
	ranges := map[string]time.Duration{
		"1h":  time.Hour,
		"6h":  6 * time.Hour,
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"30d": 30 * 24 * time.Hour,
	}
	window, ok := ranges[rangeName]
	if !ok {
		window = ranges["24h"]
	}
	since := time.Now().UTC().Add(-window).Format(time.RFC3339Nano)
	rows, err := r.db.Query(`
		SELECT metric, target, latency_ms, value, data, success, error, created_at
		FROM probe_results
		WHERE node_id = ? AND metric = ? AND created_at >= ?
		ORDER BY created_at ASC
	`, nodeID, metric, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]MetricSummary, 0)
	for rows.Next() {
		summary, err := scanMetric(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, rows.Err()
}

func (r *Repository) RecentErrors(nodeID int, limit int) ([]MetricSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.Query(`
		SELECT metric, target, latency_ms, value, data, success, error, created_at
		FROM probe_results
		WHERE node_id = ? AND success = 0
		ORDER BY created_at DESC
		LIMIT ?
	`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]MetricSummary, 0)
	for rows.Next() {
		summary, err := scanMetric(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, rows.Err()
}

func (r *Repository) DelaySamples(nodeID int, limit int) ([]float64, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.Query(`
		SELECT value
		FROM (
			SELECT id, COALESCE(value, latency_ms) AS value
			FROM probe_results
			WHERE node_id = ? AND metric = 'delay' AND success = 1
			      AND COALESCE(value, latency_ms) IS NOT NULL
			ORDER BY id DESC
			LIMIT ?
		)
		ORDER BY id ASC
	`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]float64, 0, limit)
	for rows.Next() {
		var value float64
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r *Repository) SaveProbeBatch(nodeID int, results []ProbeResultInput) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	successCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		}
		if _, err := tx.Exec(`
			INSERT INTO probe_results
				(node_id, metric, target, latency_ms, value, data, success, error, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, nodeID, result.Metric, result.Target, nullableFloat(result.LatencyMS), nullableFloat(result.Value), nullableString(result.Data), result.Success, nullableString(result.Error), timestamp); err != nil {
			return err
		}
	}

	status := "down"
	if successCount > 0 {
		status = "available"
	}
	if _, err := tx.Exec(`
		UPDATE nodes
		SET status = ?, last_checked_at = ?, updated_at = ?
		WHERE id = ?
	`, status, timestamp, timestamp, nodeID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) taskNodeCounts() (map[int]int, error) {
	rows, err := r.db.Query(`
		SELECT task_id, COUNT(id)
		FROM nodes
		WHERE task_id IS NOT NULL AND status != 'removed'
		GROUP BY task_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[int]int{}
	for rows.Next() {
		var taskID, count int
		if err := rows.Scan(&taskID, &count); err != nil {
			return nil, err
		}
		counts[taskID] = count
	}
	return counts, rows.Err()
}

func scanTask(row scanner) (Task, error) {
	var task Task
	var enabled, advancedProbesEnabled sql.NullBool
	var lastRefreshAt, lastRefreshError, lastCheckedAt, nextRunAt sql.NullString
	if err := row.Scan(
		&task.ID,
		&task.Name,
		&task.SourceURL,
		&enabled,
		&task.IntervalSeconds,
		&advancedProbesEnabled,
		&task.Status,
		&task.ConfigPath,
		&lastRefreshAt,
		&lastRefreshError,
		&lastCheckedAt,
		&nextRunAt,
	); err != nil {
		return Task{}, err
	}
	task.Enabled = enabled.Bool
	task.AdvancedProbesEnabled = advancedProbesEnabled.Bool
	task.LastRefreshAt = stringPtr(lastRefreshAt)
	task.LastRefreshError = stringPtr(lastRefreshError)
	task.LastCheckedAt = stringPtr(lastCheckedAt)
	task.NextRunAt = stringPtr(nextRunAt)
	return task, nil
}

func (r *Repository) fillNode(node *Node) error {
	metrics, err := r.latestMetrics(node.ID)
	if err != nil {
		return err
	}
	node.Metrics = metrics
	meta, err := r.nodeMeta(node.ID)
	if err != nil {
		return err
	}
	node.Meta = meta
	score, confidence, breakdown := ScoreNode(*node)
	node.Score = score
	node.Confidence = confidence
	node.Breakdown = breakdown
	return nil
}

func (r *Repository) latestMetrics(nodeID int) (map[string]MetricSummary, error) {
	rows, err := r.db.Query(`
		SELECT pr.metric, pr.target, pr.latency_ms, pr.value, pr.data, pr.success, pr.error, pr.created_at
		FROM probe_results pr
		JOIN (
			SELECT metric, MAX(id) AS result_id
			FROM probe_results
			WHERE node_id = ?
			GROUP BY metric
		) latest ON latest.result_id = pr.id
		ORDER BY pr.metric ASC
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics := map[string]MetricSummary{}
	for rows.Next() {
		summary, err := scanMetric(rows)
		if err != nil {
			return nil, err
		}
		metrics[summary.Metric] = summary
	}
	return metrics, rows.Err()
}

func (r *Repository) nodeMeta(nodeID int) (*NodeMeta, error) {
	row := r.db.QueryRow(`
		SELECT exit_ip, asn, country, region, isp,
		       netflix_unlock, disney_unlock, openai_unlock, youtube_unlock, dns_leak
		FROM node_meta
		WHERE node_id = ?
	`, nodeID)
	var meta NodeMeta
	var exitIP, asn, country, region, isp sql.NullString
	var netflix, disney, openai, youtube, dnsLeak sql.NullString
	if err := row.Scan(&exitIP, &asn, &country, &region, &isp, &netflix, &disney, &openai, &youtube, &dnsLeak); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	meta.ExitIP = stringPtr(exitIP)
	meta.ASN = stringPtr(asn)
	meta.Country = stringPtr(country)
	meta.Region = stringPtr(region)
	meta.ISP = stringPtr(isp)
	meta.NetflixUnlock = stringPtr(netflix)
	meta.DisneyUnlock = stringPtr(disney)
	meta.OpenAIUnlock = stringPtr(openai)
	meta.YouTubeUnlock = stringPtr(youtube)
	meta.DNSLeak = stringPtr(dnsLeak)
	return &meta, nil
}

func (r *Repository) UpsertNodeMeta(nodeID int, meta NodeMeta) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var id int
	var current NodeMeta
	var exitIP, asn, country, region, isp sql.NullString
	var netflix, disney, openai, youtube, dnsLeak sql.NullString
	err := r.db.QueryRow(`
		SELECT id, exit_ip, asn, country, region, isp,
		       netflix_unlock, disney_unlock, openai_unlock, youtube_unlock, dns_leak
		FROM node_meta
		WHERE node_id = ?
		ORDER BY id ASC
		LIMIT 1
	`, nodeID).Scan(&id, &exitIP, &asn, &country, &region, &isp, &netflix, &disney, &openai, &youtube, &dnsLeak)
	if err == sql.ErrNoRows {
		_, err = r.db.Exec(`
			INSERT INTO node_meta
				(node_id, exit_ip, asn, country, region, isp,
				 netflix_unlock, disney_unlock, openai_unlock, youtube_unlock,
				 dns_leak, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, nodeID, nullableString(meta.ExitIP), nullableString(meta.ASN), nullableString(meta.Country), nullableString(meta.Region), nullableString(meta.ISP), nullableString(meta.NetflixUnlock), nullableString(meta.DisneyUnlock), nullableString(meta.OpenAIUnlock), nullableString(meta.YouTubeUnlock), nullableString(meta.DNSLeak), now)
		return err
	}
	if err != nil {
		return err
	}
	current = NodeMeta{
		ExitIP:        stringPtr(exitIP),
		ASN:           stringPtr(asn),
		Country:       stringPtr(country),
		Region:        stringPtr(region),
		ISP:           stringPtr(isp),
		NetflixUnlock: stringPtr(netflix),
		DisneyUnlock:  stringPtr(disney),
		OpenAIUnlock:  stringPtr(openai),
		YouTubeUnlock: stringPtr(youtube),
		DNSLeak:       stringPtr(dnsLeak),
	}
	meta = mergeNodeMeta(current, meta)
	_, err = r.db.Exec(`
		UPDATE node_meta
		SET exit_ip = ?, asn = ?, country = ?, region = ?, isp = ?,
		    netflix_unlock = ?, disney_unlock = ?, openai_unlock = ?,
		    youtube_unlock = ?, dns_leak = ?, updated_at = ?
		WHERE id = ?
	`, nullableString(meta.ExitIP), nullableString(meta.ASN), nullableString(meta.Country), nullableString(meta.Region), nullableString(meta.ISP), nullableString(meta.NetflixUnlock), nullableString(meta.DisneyUnlock), nullableString(meta.OpenAIUnlock), nullableString(meta.YouTubeUnlock), nullableString(meta.DNSLeak), now, id)
	return err
}

func mergeNodeMeta(current NodeMeta, patch NodeMeta) NodeMeta {
	return NodeMeta{
		ExitIP:        firstStringPtr(patch.ExitIP, current.ExitIP),
		ASN:           firstStringPtr(patch.ASN, current.ASN),
		Country:       firstStringPtr(patch.Country, current.Country),
		Region:        firstStringPtr(patch.Region, current.Region),
		ISP:           firstStringPtr(patch.ISP, current.ISP),
		NetflixUnlock: firstStringPtr(patch.NetflixUnlock, current.NetflixUnlock),
		DisneyUnlock:  firstStringPtr(patch.DisneyUnlock, current.DisneyUnlock),
		OpenAIUnlock:  firstStringPtr(patch.OpenAIUnlock, current.OpenAIUnlock),
		YouTubeUnlock: firstStringPtr(patch.YouTubeUnlock, current.YouTubeUnlock),
		DNSLeak:       firstStringPtr(patch.DNSLeak, current.DNSLeak),
	}
}

func firstStringPtr(primary *string, fallback *string) *string {
	if primary != nil {
		return primary
	}
	return fallback
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNode(row scanner) (Node, error) {
	var node Node
	var taskID, port, listenerPort sql.NullInt64
	var typ, server, rawConfig, lastChecked sql.NullString
	if err := row.Scan(
		&node.ID,
		&taskID,
		&node.Name,
		&typ,
		&server,
		&port,
		&rawConfig,
		&listenerPort,
		&node.Status,
		&lastChecked,
	); err != nil {
		return Node{}, err
	}
	node.TaskID = intPtr(taskID)
	node.Type = stringPtr(typ)
	node.Server = stringPtr(server)
	node.Port = intPtr(port)
	node.RawConfig = stringPtr(rawConfig)
	node.ListenerPort = intPtr(listenerPort)
	node.LastCheckedAt = stringPtr(lastChecked)
	return node, nil
}

func scanMetric(row scanner) (MetricSummary, error) {
	var summary MetricSummary
	var latency, value sql.NullFloat64
	var data, errText sql.NullString
	var success sql.NullBool
	if err := row.Scan(
		&summary.Metric,
		&summary.Target,
		&latency,
		&value,
		&data,
		&success,
		&errText,
		&summary.CreatedAt,
	); err != nil {
		return MetricSummary{}, err
	}
	summary.LatencyMS = floatPtr(latency)
	summary.Value = floatPtr(value)
	summary.Data = stringPtr(data)
	summary.Success = success.Bool
	summary.Error = stringPtr(errText)
	return summary, nil
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func intPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	item := int(value.Int64)
	return &item
}

func floatPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullableString(value any) any {
	switch item := value.(type) {
	case nil:
		return nil
	case *string:
		if item == nil {
			return nil
		}
		return *item
	case string:
		if item == "" {
			return nil
		}
		return item
	default:
		return item
	}
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nextFreePort(used map[int]struct{}, start, max int) (int, error) {
	for port := start; port <= max; port++ {
		if _, ok := used[port]; !ok {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free listener port in range %d-%d", start, max)
}

func metricValue(summary MetricSummary) *float64 {
	if summary.LatencyMS != nil {
		return summary.LatencyMS
	}
	return summary.Value
}

func ScoreNode(node Node) (*float64, float64, map[string]ScoreComponent) {
	breakdown := map[string]ScoreComponent{}
	statusScore := map[string]float64{
		"available": 100,
		"unknown":   50,
		"down":      0,
		"removed":   0,
	}[node.Status]
	breakdown["status"] = scoreComponent(10, statusScore, nil, node.Status)

	if delay, ok := node.Metrics["delay"]; ok {
		value := metricValue(delay)
		score := 0.0
		if delay.Success && value != nil {
			score = latencyScore(*value, 100, 1000)
		}
		breakdown["delay"] = scoreComponent(35, score, value, statusText(delay.Success))
	}
	if packetLoss, ok := node.Metrics["packet_loss"]; ok {
		value := metricValue(packetLoss)
		score := 0.0
		if packetLoss.Success && value != nil {
			score = clamp(100 - (*value * 4))
		}
		breakdown["packet_loss"] = scoreComponent(25, score, value, statusText(packetLoss.Success))
	}
	if jitter, ok := node.Metrics["jitter"]; ok {
		value := metricValue(jitter)
		score := 0.0
		if jitter.Success && value != nil {
			score = latencyScore(*value, 20, 200)
		}
		breakdown["jitter"] = scoreComponent(15, score, value, statusText(jitter.Success))
	}

	var transportScores []float64
	var transportValues []float64
	transportFailed := false
	for _, metric := range []string{"tcping", "http_rtt", "tls_handshake"} {
		summary, ok := node.Metrics[metric]
		if !ok {
			continue
		}
		value := metricValue(summary)
		if !summary.Success || value == nil {
			transportFailed = true
			transportScores = append(transportScores, 0)
			continue
		}
		transportValues = append(transportValues, *value)
		transportScores = append(transportScores, latencyScore(*value, 100, 1500))
	}
	if len(transportScores) > 0 {
		avgScore := average(transportScores)
		var avgValue *float64
		if len(transportValues) > 0 {
			v := average(transportValues)
			avgValue = &v
		}
		status := "ok"
		if transportFailed && len(transportValues) == 0 {
			status = "failed"
		}
		breakdown["transport"] = scoreComponent(15, avgScore, avgValue, status)
	}
	if bandwidth, ok := node.Metrics["miaospeed_bandwidth"]; ok {
		value := metricValue(bandwidth)
		score := 0.0
		if bandwidth.Success && value != nil {
			score = throughputScore(*value, 5, 100)
		}
		breakdown["bandwidth"] = scoreComponent(10, score, value, statusText(bandwidth.Success))
	}
	if node.Meta != nil && node.Meta.DNSLeak != nil && strings.TrimSpace(*node.Meta.DNSLeak) != "" {
		score, status := dnsLeakScore(*node.Meta.DNSLeak)
		breakdown["dns_leak"] = scoreComponent(15, score, nil, status)
	}
	if node.Meta != nil {
		if score, status, ok := unlockScore(*node.Meta); ok {
			breakdown["unlock"] = scoreComponent(5, score, nil, status)
		}
	}

	availableWeight := 0.0
	weightedScore := 0.0
	for _, component := range breakdown {
		availableWeight += component.Weight
		weightedScore += component.Score * component.Weight
	}
	if availableWeight <= 0 {
		return nil, 0, map[string]ScoreComponent{}
	}
	score := round(weightedScore / availableWeight)
	confidence := round(math.Min(1, availableWeight/100))
	return &score, confidence, breakdown
}

func scoreComponent(weight, score float64, value *float64, status string) ScoreComponent {
	return ScoreComponent{
		Weight:       weight,
		Score:        round(score),
		Contribution: round(score * weight / 100),
		Value:        value,
		Status:       status,
	}
}

func statusText(success bool) string {
	if success {
		return "ok"
	}
	return "failed"
}

func latencyScore(value, excellent, poor float64) float64 {
	if value <= excellent {
		return 100
	}
	if value >= poor {
		return 0
	}
	return clamp(((poor - value) / (poor - excellent)) * 100)
}

func throughputScore(value, poor, excellent float64) float64 {
	if value >= excellent {
		return 100
	}
	if value <= poor {
		return 0
	}
	return clamp(((value - poor) / (excellent - poor)) * 100)
}

func dnsLeakScore(value string) (float64, string) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return 50, "unknown"
	}
	cleanTokens := []string{"clean", "no leak", "no_leak", "safe", "normal", "ok", "false"}
	for _, token := range cleanTokens {
		if strings.Contains(normalized, token) {
			return 100, "clean"
		}
	}
	leakedTokens := []string{"leak", "dirty", "pollut", "unsafe", "true"}
	for _, token := range leakedTokens {
		if strings.Contains(normalized, token) {
			return 0, "leaked"
		}
	}
	return 50, normalized
}

func unlockScore(meta NodeMeta) (float64, string, bool) {
	items := []*string{
		meta.NetflixUnlock,
		meta.DisneyUnlock,
		meta.OpenAIUnlock,
		meta.YouTubeUnlock,
	}
	scores := []float64{}
	unlocked := 0
	for _, item := range items {
		if item == nil || strings.TrimSpace(*item) == "" {
			continue
		}
		score := unlockStatusScore(*item)
		if score >= 75 {
			unlocked++
		}
		scores = append(scores, score)
	}
	if len(scores) == 0 {
		return 0, "", false
	}
	return average(scores), fmt.Sprintf("%d/%d", unlocked, len(scores)), true
}

func unlockStatusScore(value string) float64 {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return 50
	}
	blockedTokens := []string{"block", "unavailable", "unsupported", "deny", "denied", "fail", "false", "no"}
	for _, token := range blockedTokens {
		if strings.Contains(normalized, token) {
			return 0
		}
	}
	fullTokens := []string{"full", "available", "unlock", "unlocked", "allow", "allowed", "support", "yes", "true", "ok"}
	for _, token := range fullTokens {
		if strings.Contains(normalized, token) {
			return 100
		}
	}
	if len(normalized) == 2 && normalized[0] >= 'a' && normalized[0] <= 'z' && normalized[1] >= 'a' && normalized[1] <= 'z' {
		return 100
	}
	partialTokens := []string{"partial", "limited", "region"}
	for _, token := range partialTokens {
		if strings.Contains(normalized, token) {
			return 60
		}
	}
	return 50
}

func clamp(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func round(value float64) float64 {
	return math.Round(value*100) / 100
}

func average(values []float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func FormatFloat(value float64) string {
	if math.Mod(value, 1) == 0 {
		return fmt.Sprintf("%.0f", value)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", value), "0"), ".")
}
