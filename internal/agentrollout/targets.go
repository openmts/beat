package agentrollout

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (tx *transaction) snapshotTargets(input CreateInput) ([]Target, error) {
	count := len(input.NodeIDs) + len(input.GroupIDs) + len(input.Tags)
	if count == 0 || count > maximumTargets {
		return nil, ErrInvalid
	}
	if err := tx.requireTargets("nodes", input.NodeIDs); err != nil {
		return nil, err
	}
	if err := tx.requireTargets("groups", input.GroupIDs); err != nil {
		return nil, err
	}
	filter, args, err := targetFilter(input)
	if err != nil {
		return nil, err
	}
	rows, err := tx.db.QueryContext(tx.ctx, "SELECT id,name,agent_version,os,arch FROM nodes WHERE "+filter+" ORDER BY id LIMIT ?", append(args, maximumTargets+1)...)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = rows.Close() }()
	return readTargets(rows)
}

func (tx *transaction) requireTargets(table string, ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > maximumIdentifierBytes || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
		var count int
		if err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM "+table+" WHERE id=?", id).Scan(&count); err != nil {
			return ErrUnavailable
		}
		if count != 1 {
			return ErrNotFound
		}
	}
	return nil
}

func (tx *transaction) rollout(id string) (Rollout, error) {
	var rollout Rollout
	var raw string
	err := tx.db.QueryRowContext(tx.ctx, "SELECT action,id,release_id,name,state,reason,policy_json,current_wave,total_waves FROM agent_rollouts WHERE id=?", id).Scan(&rollout.Action, &rollout.ID, &rollout.ReleaseID, &rollout.Name, &rollout.State, &rollout.Reason, &raw, &rollout.CurrentWave, &rollout.TotalWaves)
	if errors.Is(err, sql.ErrNoRows) {
		return rollout, ErrNotFound
	}
	if err != nil {
		return rollout, ErrUnavailable
	}
	if err := json.Unmarshal([]byte(raw), &rollout.Policy); err != nil {
		return rollout, ErrUnavailable
	}
	return rollout, nil
}

func (s *Service) Detail(ctx context.Context, actor Actor, id string) (Rollout, []Target, error) {
	var rollout Rollout
	targets := []Target{}
	err := s.read(ctx, actor, func(tx *transaction) error {
		var err error
		rollout, err = tx.rollout(id)
		rollout.ServerTime = &tx.at
		if err != nil {
			return err
		}
		rows, err := tx.db.QueryContext(ctx, "SELECT id,node_snapshot_id,node_name,source_version,os,arch,wave,state,reason,healthy_since,last_report_at,lease_expires_at FROM agent_rollout_targets WHERE rollout_id=? ORDER BY wave,node_snapshot_id", id)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var target Target
			if err := rows.Scan(&target.ID, &target.NodeID, &target.Name, &target.Version, &target.OS, &target.Arch, &target.Wave, &target.State, &target.Reason, &target.HealthySince, &target.LastReportAt, &target.LeaseExpiresAt); err != nil {
				return ErrUnavailable
			}
			targets = append(targets, target)
		}
		return rows.Err()
	})
	if err != nil {
		return Rollout{}, nil, err
	}
	return rollout, targets, nil
}

func targetFilter(input CreateInput) (string, []any, error) {
	filters, args := []string{}, []any{}
	for _, entry := range []struct {
		field  string
		values []string
	}{{"id", input.NodeIDs}, {"group_id", input.GroupIDs}, {"tag", input.Tags}} {
		if len(entry.values) == 0 {
			continue
		}
		marks := make([]string, len(entry.values))
		for index, value := range entry.values {
			if value == "" || len(value) > maximumIdentifierBytes {
				return "", nil, ErrInvalid
			}
			marks[index] = "?"
			args = append(args, value)
		}
		clause := entry.field + " IN (" + strings.Join(marks, ",") + ")"
		if entry.field == "tag" {
			clause = "EXISTS(SELECT 1 FROM json_each(nodes.tags) WHERE value IN (" + strings.Join(marks, ",") + "))"
		}
		filters = append(filters, clause)
	}
	return strings.Join(filters, " OR "), args, nil
}

func readTargets(rows *sql.Rows) ([]Target, error) {
	targets := []Target{}
	for rows.Next() {
		var target Target
		if err := rows.Scan(&target.NodeID, &target.Name, &target.Version, &target.OS, &target.Arch); err != nil {
			return nil, ErrUnavailable
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrUnavailable
	}
	if len(targets) == 0 || len(targets) > maximumTargets {
		return nil, ErrInvalid
	}
	return targets, nil
}
