package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type enrollmentAudit struct {
	record enrollmentRecord
	input  EnrollmentConsumeInput
	nodeID string
	now    time.Time
}

func recordEnrollmentAudit(ctx context.Context, db schemaExecutor, audit enrollmentAudit) error {
	detail, err := json.Marshal(map[string]string{"enrollment_id": audit.record.ID, "issued_by": audit.record.CreatorID})
	if err != nil {
		return fmt.Errorf("encode enrollment audit: %w", err)
	}
	requestID := audit.input.AuditRequestID
	if requestID == "" {
		requestID = uuid.NewString()
	}
	_, err = db.ExecContext(ctx, `INSERT INTO admin_audit_events
		(id, request_id, actor_id, actor_username, action, resource_type, resource_id, outcome,
		detail_json, ip_address, user_agent, session_prefix, created_at)
		VALUES (?, ?, NULL, 'agent', 'agent.enrollment.consume', 'node', ?, 'success', ?, ?, '', '', ?)`,
		uuid.NewString(), requestID, audit.nodeID, string(detail), audit.input.SourceIP, audit.now)
	if err != nil {
		return fmt.Errorf("record enrollment audit: %w", err)
	}
	return nil
}
