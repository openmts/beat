package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func supersedeCustomMetricTasks(ctx context.Context, db schemaExecutor, replacement model.AgentTask) error {
	result, err := db.ExecContext(ctx, `UPDATE agent_tasks SET status=?,completed_at=? WHERE node_id=? AND type=? AND status=? AND id<>?`,
		model.AgentTaskStatusFailed, replacement.CreatedAt, replacement.NodeID, model.AgentTaskCustomMetricRegister, model.AgentTaskStatusPending, replacement.ID)
	if err != nil {
		return fmt.Errorf("supersede custom metric snapshots: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count superseded custom metric snapshots: %w", err)
	}
	if count == 0 {
		return nil
	}
	detail, err := json.Marshal(map[string]any{"node_id": replacement.NodeID, "superseded_count": count, "reason": "replaced_by_newer_full_snapshot"})
	if err != nil {
		return fmt.Errorf("encode custom metric supersession audit: %w", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO admin_audit_events(id,request_id,actor_id,actor_username,action,resource_type,resource_id,
		outcome,detail_json,ip_address,user_agent,session_prefix,created_at) VALUES(?,?,NULL,'system','agent.custom_metrics.supersede','agent_task',?,'success',?,'','','',?)`,
		uuid.NewString(), replacement.ID, replacement.ID, string(detail), replacement.CreatedAt)
	if err != nil {
		return fmt.Errorf("audit superseded custom metric snapshots: %w", err)
	}
	return nil
}
