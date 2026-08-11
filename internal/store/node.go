package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

type NodeStore struct {
	db *sql.DB
}

type NodeUpdate struct {
	Alias            string
	GroupID          string
	SSHPublicKey     string
	TrafficLimit     *int64
	TrafficLimitType *string
	TrafficResetDay  *int
	SortOrder        *int
	Tags             *[]string
	IsPublic         *bool
	PublicRemark     *string
	PrivateRemark    *string
}

type NodeHeartbeat struct {
	Name   string
	Host   string
	Port   int
	System model.SystemInfo
}

const nodeSelectColumns = `id, name, alias, group_id, host, port, status, ssh_public_key,
	cpu_model, os, platform, os_version, kernel, arch, virtualization, agent_version,
	sort_order, tags, is_public, public_remark, private_remark,
	agent_token_hash, agent_token_prefix, agent_token_created_at, agent_token_last_used_at,
	agent_token_revoked_at,
	traffic_limit, traffic_limit_type, traffic_reset_day,
	last_seen, created_at, updated_at`

type rowScanner interface {
	Scan(...any) error
}

func NewNodeStore(db *sql.DB) *NodeStore {
	return &NodeStore{db: db}
}

func (s *NodeStore) ListNodes(ctx context.Context, groupID string) ([]model.Node, error) {
	return s.listNodes(ctx, groupID, false)
}

func (s *NodeStore) ListPublicNodes(ctx context.Context, groupID string) ([]model.Node, error) {
	return s.listNodes(ctx, groupID, true)
}

func (s *NodeStore) listNodes(ctx context.Context, groupID string, publicOnly bool) ([]model.Node, error) {
	var (
		rows *sql.Rows
		err  error
	)

	switch {
	case groupID != "" && publicOnly:
		rows, err = s.db.QueryContext(ctx,
			"SELECT "+nodeSelectColumns+" FROM nodes WHERE group_id = ? AND is_public = 1 "+
				"ORDER BY sort_order ASC, name ASC", groupID)
	case groupID != "":
		rows, err = s.db.QueryContext(ctx,
			"SELECT "+nodeSelectColumns+" FROM nodes WHERE group_id = ? "+
				"ORDER BY sort_order ASC, name ASC", groupID)
	case publicOnly:
		rows, err = s.db.QueryContext(ctx,
			"SELECT "+nodeSelectColumns+" FROM nodes WHERE is_public = 1 "+
				"ORDER BY sort_order ASC, name ASC")
	default:
		rows, err = s.db.QueryContext(ctx,
			"SELECT "+nodeSelectColumns+" FROM nodes ORDER BY sort_order ASC, name ASC")
	}
	if err != nil {
		return nil, fmt.Errorf("querying nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	nodes := []model.Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning node: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating nodes: %w", err)
	}

	return nodes, nil
}

func (s *NodeStore) GetPublicNode(ctx context.Context, id string) (*model.Node, error) {
	node, err := scanNode(s.db.QueryRowContext(ctx,
		"SELECT "+nodeSelectColumns+" FROM nodes WHERE id = ? AND is_public = 1", id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying public node: %w", err)
	}
	return &node, nil
}

func (s *NodeStore) GetNode(ctx context.Context, id string) (*model.Node, error) {
	n, err := scanNode(s.db.QueryRowContext(ctx,
		"SELECT "+nodeSelectColumns+" FROM nodes WHERE id = ?", id,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying node: %w", err)
	}
	return &n, nil
}

func (s *NodeStore) GetNodeByName(ctx context.Context, name string) (*model.Node, error) {
	return s.getNodeByName(ctx, name)
}

func (s *NodeStore) UpdateNode(ctx context.Context, id string, update NodeUpdate) (*model.Node, error) {
	node, err := s.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, nil
	}

	now := model.NowUTC()
	applyTrafficUpdate(node, update)
	applyNodePresentationUpdate(node, update)
	tagsJSON, err := json.Marshal(node.Tags)
	if err != nil {
		return nil, fmt.Errorf("encoding node tags: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE nodes SET alias = ?, group_id = ?, ssh_public_key = ?, traffic_limit = ?,
		 traffic_limit_type = ?, traffic_reset_day = ?, sort_order = ?, tags = ?, is_public = ?,
		 public_remark = ?, private_remark = ?, updated_at = ? WHERE id = ?`,
		update.Alias, update.GroupID, update.SSHPublicKey, node.TrafficLimit,
		node.TrafficLimitType, node.TrafficResetDay, node.SortOrder, string(tagsJSON), node.IsPublic,
		node.PublicRemark, node.PrivateRemark, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating node: %w", err)
	}

	node.Alias = update.Alias
	node.GroupID = update.GroupID
	node.SSHPublicKey = update.SSHPublicKey
	node.UpdatedAt = now

	return node, nil
}

func (s *NodeStore) DeleteNode(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting node: %w", err)
	}

	return nil
}

func (s *NodeStore) GetNodeMetrics(ctx context.Context, nodeID, metric string, from, to time.Time) ([]model.MetricData, error) {
	return nil, nil
}

func (s *NodeStore) ListOnlineNodes(ctx context.Context) ([]model.Node, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+nodeSelectColumns+" FROM nodes WHERE status = ? ORDER BY sort_order ASC, name ASC",
		model.NodeStatusOnline,
	)
	if err != nil {
		return nil, fmt.Errorf("querying online nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	nodes := []model.Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning node: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating nodes: %w", err)
	}

	return nodes, nil
}

func scanNode(scanner rowScanner) (model.Node, error) {
	var node model.Node
	var lastSeen sql.NullTime
	var tokenCreatedAt, tokenLastUsedAt, tokenRevokedAt sql.NullTime
	var tagsJSON string
	err := scanner.Scan(
		&node.ID, &node.Name, &node.Alias, &node.GroupID, &node.Host, &node.Port,
		&node.Status, &node.SSHPublicKey, &node.CPUModel, &node.OS, &node.Platform,
		&node.OSVersion, &node.Kernel, &node.Arch, &node.Virtualization, &node.AgentVersion,
		&node.SortOrder, &tagsJSON, &node.IsPublic, &node.PublicRemark, &node.PrivateRemark,
		&node.AgentTokenHash, &node.AgentTokenPrefix, &tokenCreatedAt, &tokenLastUsedAt,
		&tokenRevokedAt,
		&node.TrafficLimit, &node.TrafficLimitType, &node.TrafficResetDay,
		&lastSeen, &node.CreatedAt, &node.UpdatedAt,
	)
	if err != nil {
		return node, err
	}
	if err := json.Unmarshal([]byte(tagsJSON), &node.Tags); err != nil {
		return node, fmt.Errorf("decode node tags: %w", err)
	}
	if node.Tags == nil {
		node.Tags = []string{}
	}
	if lastSeen.Valid {
		node.LastSeen = lastSeen.Time
	}
	node.AgentTokenCreatedAt = nullableTime(tokenCreatedAt)
	node.AgentTokenLastUsedAt = nullableTime(tokenLastUsedAt)
	node.AgentTokenRevokedAt = nullableTime(tokenRevokedAt)
	return node, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func applyTrafficUpdate(node *model.Node, update NodeUpdate) {
	if update.TrafficLimit != nil {
		node.TrafficLimit = *update.TrafficLimit
	}
	if update.TrafficLimitType != nil {
		node.TrafficLimitType = *update.TrafficLimitType
	}
	if update.TrafficResetDay != nil {
		node.TrafficResetDay = *update.TrafficResetDay
	}
}

func applyNodePresentationUpdate(node *model.Node, update NodeUpdate) {
	if update.SortOrder != nil {
		node.SortOrder = *update.SortOrder
	}
	if update.Tags != nil {
		node.Tags = append(make([]string, 0, len(*update.Tags)), (*update.Tags)...)
	}
	if update.IsPublic != nil {
		node.IsPublic = *update.IsPublic
	}
	if update.PublicRemark != nil {
		node.PublicRemark = *update.PublicRemark
	}
	if update.PrivateRemark != nil {
		node.PrivateRemark = *update.PrivateRemark
	}
}
