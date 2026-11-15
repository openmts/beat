package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"beat/internal/domain"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	db *sql.DB
}

var (
	ErrGroupNameConflict = errors.New("node group name already exists")
	ErrGroupNotFound     = errors.New("node group not found")
)

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, time.Now().UTC().Unix()); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) NodeCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&count)
	return count, err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) ListGroups(ctx context.Context, now time.Time) ([]domain.NodeGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, color, created_at, updated_at
		FROM node_groups
		ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]domain.NodeGroup, 0)
	index := map[string]int{}
	for rows.Next() {
		var group domain.NodeGroup
		var createdAt, updatedAt int64
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.Color, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		group.CreatedAt = time.Unix(createdAt, 0).UTC()
		group.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		index[group.ID] = len(groups)
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	nodes, err := s.ListNodes(ctx, "", now)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		position, ok := index[node.GroupID]
		if !ok {
			continue
		}
		groups[position].NodeCount++
		switch node.Status {
		case domain.NodeOnline:
			groups[position].OnlineCount++
		case domain.NodeWarning:
			groups[position].WarningCount++
		case domain.NodeOffline:
			groups[position].OfflineCount++
		}
	}
	return groups, nil
}

func (s *Store) GetGroup(ctx context.Context, id string, now time.Time) (domain.NodeGroup, error) {
	groups, err := s.ListGroups(ctx, now)
	if err != nil {
		return domain.NodeGroup{}, err
	}
	for _, group := range groups {
		if group.ID == id {
			return group, nil
		}
	}
	return domain.NodeGroup{}, ErrGroupNotFound
}

func (s *Store) CreateGroup(ctx context.Context, id, name, description, color string, now time.Time) (domain.NodeGroup, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO node_groups (id, name, description, color, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, description, color, now.Unix(), now.Unix())
	if isUniqueConstraint(err) {
		return domain.NodeGroup{}, ErrGroupNameConflict
	}
	if err != nil {
		return domain.NodeGroup{}, fmt.Errorf("create node group: %w", err)
	}
	return s.GetGroup(ctx, id, now)
}

func (s *Store) UpdateGroup(ctx context.Context, id, name, description, color string, now time.Time) (domain.NodeGroup, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE node_groups
		SET name = ?, description = ?, color = ?, updated_at = ?
		WHERE id = ?`,
		name, description, color, now.Unix(), id)
	if isUniqueConstraint(err) {
		return domain.NodeGroup{}, ErrGroupNameConflict
	}
	if err != nil {
		return domain.NodeGroup{}, fmt.Errorf("update node group: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.NodeGroup{}, err
	}
	if affected == 0 {
		return domain.NodeGroup{}, ErrGroupNotFound
	}
	return s.GetGroup(ctx, id, now)
}

func (s *Store) DeleteGroup(ctx context.Context, id string, now time.Time) (domain.NodeGroup, error) {
	group, err := s.GetGroup(ctx, id, now)
	if err != nil {
		return domain.NodeGroup{}, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_groups WHERE id = ?`, id); err != nil {
		return domain.NodeGroup{}, fmt.Errorf("delete node group: %w", err)
	}
	return group, nil
}

func (s *Store) SetGroupMembers(ctx context.Context, groupID string, nodeIDs []string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM node_groups WHERE id = ?`, groupID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ErrGroupNotFound
	} else if err != nil {
		return err
	}
	uniqueNodes := make([]string, 0, len(nodeIDs))
	seen := make(map[string]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id = ?`, nodeID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("node %s: %w", nodeID, sql.ErrNoRows)
		} else if err != nil {
			return err
		}
		uniqueNodes = append(uniqueNodes, nodeID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_group_members WHERE group_id = ?`, groupID); err != nil {
		return err
	}
	for _, nodeID := range uniqueNodes {
		if _, err := tx.ExecContext(ctx, `DELETE FROM node_group_members WHERE node_id = ?`, nodeID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO node_group_members (node_id, group_id, assigned_at)
			VALUES (?, ?, ?)`, nodeID, groupID, now.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertNode(ctx context.Context, report domain.Report, now time.Time) (domain.Node, error) {
	var id string
	var firstSeen int64
	err := s.db.QueryRowContext(ctx, `SELECT id, first_seen FROM nodes WHERE agent_id = ?`, report.AgentID).Scan(&id, &firstSeen)
	if errors.Is(err, sql.ErrNoRows) {
		id = uuid.NewString()
		firstSeen = now.Unix()
	} else if err != nil {
		return domain.Node{}, err
	}
	labels, _ := json.Marshal(nonNilLabels(report.Labels))
	interfaces, _ := json.Marshal(report.Interfaces)
	metrics, _ := json.Marshal(report.Metrics)
	status := string(domain.NodeOnline)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO nodes (
			id, agent_id, hostname, address, os, kernel, arch, agent_version, status,
			labels_json, interfaces_json, cpu_cores, cpu_model, total_memory_bytes,
			total_disk_bytes, collector_interval_seconds, latest_metrics_json, first_seen, last_seen
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			hostname = excluded.hostname,
			address = excluded.address,
			os = excluded.os,
			kernel = excluded.kernel,
			arch = excluded.arch,
			agent_version = excluded.agent_version,
			status = excluded.status,
			labels_json = excluded.labels_json,
			interfaces_json = excluded.interfaces_json,
			cpu_cores = excluded.cpu_cores,
			cpu_model = excluded.cpu_model,
			total_memory_bytes = excluded.total_memory_bytes,
			total_disk_bytes = excluded.total_disk_bytes,
			collector_interval_seconds = excluded.collector_interval_seconds,
			latest_metrics_json = excluded.latest_metrics_json,
			last_seen = excluded.last_seen`,
		id, report.AgentID, report.Hostname, report.Address, report.OS, report.Kernel,
		report.Arch, report.AgentVersion, status, string(labels), string(interfaces),
		report.CPUCores, report.CPUModel, report.Metrics.MemoryTotalBytes,
		report.Metrics.DiskTotalBytes, normalizeInterval(report.IntervalSeconds),
		string(metrics), firstSeen, now.Unix()); err != nil {
		return domain.Node{}, fmt.Errorf("upsert node: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, node_id, version, interval_seconds, last_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			node_id = excluded.node_id,
			version = excluded.version,
			interval_seconds = excluded.interval_seconds,
			last_seen = excluded.last_seen`,
		report.AgentID, id, report.AgentVersion, normalizeInterval(report.IntervalSeconds), now.Unix(), now.Unix()); err != nil {
		return domain.Node{}, fmt.Errorf("upsert agent: %w", err)
	}
	return s.GetNode(ctx, id, now)
}

func (s *Store) GetNode(ctx context.Context, id string, now time.Time) (domain.Node, error) {
	row := s.db.QueryRowContext(ctx, nodeSelect+` WHERE id = ?`, id)
	node, err := scanNode(row)
	if err != nil {
		return domain.Node{}, err
	}
	node, err = s.applyNodeConfig(ctx, node)
	if err != nil {
		return domain.Node{}, err
	}
	return normalizeNode(node, now), nil
}

func (s *Store) GetNodeByAgent(ctx context.Context, agentID string, now time.Time) (domain.Node, error) {
	row := s.db.QueryRowContext(ctx, nodeSelect+` WHERE agent_id = ?`, agentID)
	node, err := scanNode(row)
	if err != nil {
		return domain.Node{}, err
	}
	node, err = s.applyNodeConfig(ctx, node)
	if err != nil {
		return domain.Node{}, err
	}
	return normalizeNode(node, now), nil
}

func (s *Store) ListNodes(ctx context.Context, search string, now time.Time) ([]domain.Node, error) {
	rows, err := s.db.QueryContext(ctx, nodeSelect+` ORDER BY hostname ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]domain.Node, 0)
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
	if err := rows.Close(); err != nil {
		return nil, err
	}
	search = strings.ToLower(strings.TrimSpace(search))
	filtered := make([]domain.Node, 0, len(nodes))
	for _, node := range nodes {
		node, err := s.applyNodeConfig(ctx, node)
		if err != nil {
			return nil, err
		}
		node = normalizeNode(node, now)
		if search != "" {
			haystack := node.Hostname + " " + node.Address + " " + node.OS + " " + node.AgentID
			for key, value := range node.Labels {
				haystack += " " + key + "=" + value
			}
			if !strings.Contains(strings.ToLower(haystack), search) {
				continue
			}
		}
		filtered = append(filtered, node)
	}
	return filtered, nil
}

func (s *Store) ListAgents(ctx context.Context, now time.Time) ([]domain.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.node_id, COALESCE(NULLIF(nc.hostname, ''), n.hostname), a.version,
		       a.interval_seconds, a.last_seen, a.created_at
		FROM agents a
		JOIN nodes n ON n.id = a.node_id
		LEFT JOIN node_configs nc ON nc.node_id = n.id
		ORDER BY n.hostname ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	agents := make([]domain.Agent, 0)
	for rows.Next() {
		var agent domain.Agent
		var lastSeen, createdAt int64
		if err := rows.Scan(&agent.ID, &agent.NodeID, &agent.NodeName, &agent.Version, &agent.IntervalSeconds, &lastSeen, &createdAt); err != nil {
			return nil, err
		}
		agent.LastSeen = time.Unix(lastSeen, 0).UTC()
		agent.CreatedAt = time.Unix(createdAt, 0).UTC()
		agent.Status = statusForAgent(agent, now)
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (s *Store) UpdateNodeConfig(ctx context.Context, nodeID, hostname string, labels map[string]string, groupID string, now time.Time) (domain.Node, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Node{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id = ?`, nodeID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return domain.Node{}, sql.ErrNoRows
	} else if err != nil {
		return domain.Node{}, err
	}
	if groupID != "" {
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM node_groups WHERE id = ?`, groupID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return domain.Node{}, ErrGroupNotFound
		} else if err != nil {
			return domain.Node{}, err
		}
	}
	labelsJSON, err := json.Marshal(nonNilLabels(labels))
	if err != nil {
		return domain.Node{}, fmt.Errorf("encode node labels: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO node_configs (node_id, hostname, labels_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			hostname = excluded.hostname,
			labels_json = excluded.labels_json,
			updated_at = excluded.updated_at`,
		nodeID, hostname, string(labelsJSON), now.Unix()); err != nil {
		return domain.Node{}, fmt.Errorf("update node config: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_group_members WHERE node_id = ?`, nodeID); err != nil {
		return domain.Node{}, fmt.Errorf("clear node group: %w", err)
	}
	if groupID != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO node_group_members (node_id, group_id, assigned_at)
			VALUES (?, ?, ?)`, nodeID, groupID, now.Unix()); err != nil {
			return domain.Node{}, fmt.Errorf("assign node group: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Node{}, err
	}
	return s.GetNode(ctx, nodeID, now)
}

func (s *Store) UninstallNode(ctx context.Context, nodeID string, now time.Time) (domain.Node, domain.AgentTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Node{}, domain.AgentTask{}, err
	}
	defer func() { _ = tx.Rollback() }()

	node, err := scanNode(tx.QueryRowContext(ctx, nodeSelect+` WHERE id = ?`, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Node{}, domain.AgentTask{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.Node{}, domain.AgentTask{}, err
	}
	var configuredHostname, configuredLabelsJSON string
	switch err := tx.QueryRowContext(ctx, `SELECT hostname, labels_json FROM node_configs WHERE node_id = ?`, node.ID).Scan(&configuredHostname, &configuredLabelsJSON); {
	case err == nil:
		if strings.TrimSpace(configuredHostname) != "" {
			node.Hostname = configuredHostname
		}
		var configuredLabels map[string]string
		if json.Unmarshal([]byte(configuredLabelsJSON), &configuredLabels) == nil && configuredLabels != nil {
			node.Labels = configuredLabels
		}
	case !errors.Is(err, sql.ErrNoRows):
		return domain.Node{}, domain.AgentTask{}, err
	}
	task := domain.AgentTask{
		ID:        uuid.NewString(),
		AgentID:   node.AgentID,
		NodeID:    node.ID,
		Kind:      "uninstall_agent",
		Status:    "pending",
		CreatedAt: now.UTC(),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_tasks (id, agent_id, node_id, kind, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		task.ID, task.AgentID, task.NodeID, task.Kind, task.Status, now.Unix()); err != nil {
		return domain.Node{}, domain.AgentTask{}, fmt.Errorf("create uninstall task: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_tombstones (agent_id, node_id, reason, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			node_id = excluded.node_id,
			reason = excluded.reason,
			created_at = excluded.created_at`,
		node.AgentID, node.ID, "agent_uninstalled", now.Unix()); err != nil {
		return domain.Node{}, domain.AgentTask{}, fmt.Errorf("create agent tombstone: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, node.ID); err != nil {
		return domain.Node{}, domain.AgentTask{}, fmt.Errorf("delete node: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Node{}, domain.AgentTask{}, err
	}
	return node, task, nil
}

func (s *Store) IsAgentDeleted(ctx context.Context, agentID string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM agent_tombstones WHERE agent_id = ?`, agentID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ClaimAgentTasks(ctx context.Context, agentID string, now time.Time) ([]domain.AgentTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, node_id, kind, status, created_at
		FROM agent_tasks
		WHERE agent_id = ? AND status IN ('pending', 'delivered')
		ORDER BY created_at ASC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]domain.AgentTask, 0)
	for rows.Next() {
		var task domain.AgentTask
		var createdAt int64
		if err := rows.Scan(&task.ID, &task.AgentID, &task.NodeID, &task.Kind, &task.Status, &createdAt); err != nil {
			return nil, err
		}
		task.CreatedAt = time.Unix(createdAt, 0).UTC()
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return tasks, nil
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE agent_tasks
		SET status = 'delivered', delivered_at = COALESCE(delivered_at, ?)
		WHERE agent_id = ? AND status IN ('pending', 'delivered')`, now.Unix(), agentID); err != nil {
		return nil, err
	}
	for index := range tasks {
		tasks[index].Status = "delivered"
	}
	return tasks, nil
}

func (s *Store) CompleteAgentTask(ctx context.Context, taskID, agentID, status, result string, now time.Time) (bool, error) {
	if status != "success" && status != "failed" {
		return false, fmt.Errorf("invalid task status %q", status)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	update, err := tx.ExecContext(ctx, `
		UPDATE agent_tasks
		SET status = ?, completed_at = ?, result = ?
		WHERE id = ? AND agent_id = ? AND status IN ('pending', 'delivered')`,
		status, now.Unix(), result, taskID, agentID)
	if err != nil {
		return false, err
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if status == "success" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_enrollment_tokens
			SET revoked_at = ?
			WHERE bound_agent_id = ? AND revoked_at IS NULL`, now.Unix(), agentID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) CreateEnrollmentToken(ctx context.Context, id, token, name string, createdAt, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_enrollment_tokens (id, token_hash, name, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, hashAgentToken(token), name, createdAt.Unix(), expiresAt.Unix())
	if err != nil {
		return fmt.Errorf("create enrollment token: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM agent_enrollment_tokens
		WHERE bound_agent_id IS NULL AND expires_at < ?`, createdAt.Add(-24*time.Hour).Unix())
	return nil
}

// AuthorizeEnrollmentToken validates an unbound token against its TTL and binds it
// to the first Agent that uses it. Once bound, the token remains valid only for
// that Agent so existing heartbeats continue after the enrollment window closes.
func (s *Store) AuthorizeEnrollmentToken(ctx context.Context, token, agentID string, now time.Time) (bool, error) {
	var id string
	var expiresAt int64
	var boundAgentID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, expires_at, bound_agent_id
		FROM agent_enrollment_tokens
		WHERE token_hash = ? AND revoked_at IS NULL`, hashAgentToken(token)).Scan(&id, &expiresAt, &boundAgentID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if boundAgentID.Valid {
		return boundAgentID.String == agentID, nil
	}
	if now.Unix() > expiresAt {
		return false, nil
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_enrollment_tokens
		SET bound_agent_id = ?, used_at = ?
		WHERE id = ? AND bound_agent_id IS NULL AND revoked_at IS NULL`,
		agentID, now.Unix(), id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func hashAgentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

const nodeSelect = `SELECT id, agent_id, hostname, address, os, kernel, arch, agent_version, status,
	labels_json, interfaces_json, cpu_cores, cpu_model, total_memory_bytes, total_disk_bytes,
	collector_interval_seconds, latest_metrics_json, first_seen, last_seen FROM nodes`

type scanner interface {
	Scan(dest ...any) error
}

func scanNode(row scanner) (domain.Node, error) {
	var node domain.Node
	var labelsJSON, interfacesJSON, metricsJSON string
	var firstSeen, lastSeen int64
	if err := row.Scan(&node.ID, &node.AgentID, &node.Hostname, &node.Address, &node.OS,
		&node.Kernel, &node.Arch, &node.AgentVersion, &node.Status, &labelsJSON,
		&interfacesJSON, &node.CPUCores, &node.CPUModel, &node.TotalMemoryBytes,
		&node.TotalDiskBytes, &node.CollectorIntervalSeconds, &metricsJSON,
		&firstSeen, &lastSeen); err != nil {
		return domain.Node{}, err
	}
	_ = json.Unmarshal([]byte(labelsJSON), &node.Labels)
	_ = json.Unmarshal([]byte(interfacesJSON), &node.Interfaces)
	_ = json.Unmarshal([]byte(metricsJSON), &node.Metrics)
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	if node.Interfaces == nil {
		node.Interfaces = []domain.NetworkInterface{}
	}
	node.FirstSeen = time.Unix(firstSeen, 0).UTC()
	node.LastSeen = time.Unix(lastSeen, 0).UTC()
	return node, nil
}

func (s *Store) applyNodeConfig(ctx context.Context, node domain.Node) (domain.Node, error) {
	var hostname, labelsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT hostname, labels_json FROM node_configs WHERE node_id = ?`, node.ID).Scan(&hostname, &labelsJSON)
	if err == nil {
		if strings.TrimSpace(hostname) != "" {
			node.Hostname = hostname
		}
		var labels map[string]string
		if err := json.Unmarshal([]byte(labelsJSON), &labels); err == nil && labels != nil {
			node.Labels = labels
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.Node{}, err
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT gm.group_id, g.name, g.color
		FROM node_group_members gm
		JOIN node_groups g ON g.id = gm.group_id
		WHERE gm.node_id = ?`, node.ID).Scan(&node.GroupID, &node.GroupName, &node.GroupColor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.Node{}, err
	}
	return node, nil
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func normalizeNode(node domain.Node, now time.Time) domain.Node {
	timeout := time.Duration(normalizeInterval(node.CollectorIntervalSeconds)*3) * time.Second
	if timeout < 45*time.Second {
		timeout = 45 * time.Second
	}
	if now.Sub(node.LastSeen) > timeout {
		node.Status = domain.NodeOffline
		return node
	}
	if node.Metrics.CPUUsage >= 90 || node.Metrics.MemoryUsage >= 90 || node.Metrics.DiskUsage >= 90 {
		node.Status = domain.NodeWarning
	} else {
		node.Status = domain.NodeOnline
	}
	return node
}

func statusForAgent(agent domain.Agent, now time.Time) domain.NodeStatus {
	timeout := time.Duration(normalizeInterval(agent.IntervalSeconds)*3) * time.Second
	if timeout < 45*time.Second {
		timeout = 45 * time.Second
	}
	if now.Sub(agent.LastSeen) > timeout {
		return domain.NodeOffline
	}
	return domain.NodeOnline
}

func normalizeInterval(seconds int) int {
	if seconds <= 0 {
		return 15
	}
	return seconds
}

func nonNilLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	return labels
}
