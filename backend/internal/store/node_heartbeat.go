package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (s *NodeStore) UpsertNode(ctx context.Context, name, host string, port int) (*model.Node, error) {
	return s.UpsertNodeWithSystem(ctx, NodeHeartbeat{Name: name, Host: host, Port: port})
}

func (s *NodeStore) UpsertNodeWithSystem(ctx context.Context, heartbeat NodeHeartbeat) (*model.Node, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("begin node heartbeat: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	node, err := getNodeByNameFrom(ctx, tx, heartbeat.Name)
	if err != nil {
		return nil, err
	}
	if node != nil {
		if err := updateHeartbeatWith(ctx, tx, heartbeatUpdate{node: node, heartbeat: heartbeat}); err != nil {
			return nil, err
		}
	} else {
		node, err = insertHeartbeatWith(ctx, tx, heartbeat)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit node heartbeat: %w", err)
	}
	return node, nil
}

func (s *NodeStore) UpdateNodeHeartbeat(
	ctx context.Context,
	nodeID string,
	heartbeat NodeHeartbeat,
) (*model.Node, error) {
	node, err := s.GetNode(ctx, nodeID)
	if err != nil || node == nil {
		return node, err
	}
	heartbeat.Name = node.Name
	return s.updateHeartbeat(ctx, node, heartbeat)
}

func (s *NodeStore) updateHeartbeat(
	ctx context.Context,
	node *model.Node,
	heartbeat NodeHeartbeat,
) (*model.Node, error) {
	if err := updateHeartbeatWith(ctx, s.db, heartbeatUpdate{node: node, heartbeat: heartbeat}); err != nil {
		return nil, err
	}
	return node, nil
}

type heartbeatExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type heartbeatUpdate struct {
	node      *model.Node
	heartbeat NodeHeartbeat
}

func updateHeartbeatWith(
	ctx context.Context,
	db heartbeatExecutor,
	update heartbeatUpdate,
) error {
	node := update.node
	heartbeat := update.heartbeat
	now := model.NowUTC()
	_, err := db.ExecContext(ctx,
		`UPDATE nodes SET host = ?, port = ?, status = ?, cpu_model = ?, os = ?, platform = ?,
		 os_version = ?, kernel = ?, arch = ?, virtualization = ?, agent_version = ?,
		 last_seen = ?, updated_at = ? WHERE id = ?`,
		heartbeat.Host, heartbeat.Port, model.NodeStatusOnline,
		heartbeat.System.CPUModel, heartbeat.System.OS, heartbeat.System.Platform,
		heartbeat.System.OSVersion, heartbeat.System.Kernel, heartbeat.System.Arch,
		heartbeat.System.Virtualization, heartbeat.System.AgentVersion,
		now, now, node.ID,
	)
	if err != nil {
		return fmt.Errorf("updating node heartbeat: %w", err)
	}
	node.Host = heartbeat.Host
	node.Port = heartbeat.Port
	node.Status = model.NodeStatusOnline
	applySystemInfo(node, heartbeat.System)
	node.LastSeen = now
	node.UpdatedAt = now
	return nil
}

func insertHeartbeatWith(
	ctx context.Context,
	db heartbeatExecutor,
	heartbeat NodeHeartbeat,
) (*model.Node, error) {
	groupID, err := defaultGroupIDFrom(ctx, db)
	if err != nil {
		return nil, err
	}
	now := model.NowUTC()
	node := &model.Node{
		ID: uuid.New().String(), Name: heartbeat.Name, Host: heartbeat.Host, Port: heartbeat.Port,
		Status: model.NodeStatusOnline, TrafficLimitType: model.TrafficLimitSum, TrafficResetDay: 1,
		Tags: []string{}, IsPublic: true,
		LastSeen: now, CreatedAt: now, UpdatedAt: now,
	}
	applySystemInfo(node, heartbeat.System)
	node.GroupID = groupID
	_, err = db.ExecContext(ctx,
		`INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key,
		 cpu_model, os, platform, os_version, kernel, arch, virtualization, agent_version,
		 traffic_limit, traffic_limit_type, traffic_reset_day, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		 ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.Name, node.Alias, node.GroupID, node.Host, node.Port, node.Status, node.SSHPublicKey,
		node.CPUModel, node.OS, node.Platform, node.OSVersion, node.Kernel, node.Arch,
		node.Virtualization, node.AgentVersion, node.TrafficLimit, node.TrafficLimitType,
		node.TrafficResetDay, node.LastSeen, node.CreatedAt, node.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting node: %w", err)
	}
	return node, nil
}

func (s *NodeStore) defaultGroupID(ctx context.Context) (string, error) {
	return defaultGroupIDFrom(ctx, s.db)
}

func defaultGroupIDFrom(ctx context.Context, db heartbeatExecutor) (string, error) {
	var id string
	if err := db.QueryRowContext(ctx, "SELECT id FROM groups WHERE is_default = 1 LIMIT 1").Scan(&id); err != nil {
		return "", fmt.Errorf("querying default group: %w", err)
	}
	return id, nil
}

func (s *NodeStore) getNodeByName(ctx context.Context, name string) (*model.Node, error) {
	return getNodeByNameFrom(ctx, s.db, name)
}

func getNodeByNameFrom(ctx context.Context, db heartbeatExecutor, name string) (*model.Node, error) {
	node, err := scanNode(db.QueryRowContext(ctx,
		"SELECT "+nodeSelectColumns+" FROM nodes WHERE name = ?", name,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying node by name: %w", err)
	}
	return &node, nil
}

func applySystemInfo(node *model.Node, system model.SystemInfo) {
	node.CPUModel = system.CPUModel
	node.OS = system.OS
	node.Platform = system.Platform
	node.OSVersion = system.OSVersion
	node.Kernel = system.Kernel
	node.Arch = system.Arch
	node.Virtualization = system.Virtualization
	node.AgentVersion = system.AgentVersion
}
