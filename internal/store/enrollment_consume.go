package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
)

type enrollmentRecord struct {
	ID, ServerURL, CreatorID                   string
	AllowHTTP                                  bool
	ExpiresAt                                  time.Time
	RevokedAt, ConsumedAt, CredentialCreatedAt sql.NullTime
	RequestID                                  string
	NodeID                                     sql.NullString
	AgentTokenHash                             []byte
	NodeName, Host                             string
	SSHPort                                    int
}

type enrollmentAttempt struct {
	record *enrollmentRecord
	input  EnrollmentConsumeInput
	now    time.Time
}

func (s *NodeStore) ConsumeEnrollmentToken(ctx context.Context, input EnrollmentConsumeInput) (agentjoin.Result, error) {
	input, err := prepareEnrollmentInput(input)
	if err != nil {
		return agentjoin.Result{}, err
	}
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return agentjoin.Result{}, fmt.Errorf("begin node enrollment: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if input.Now != nil {
		now = input.Now().UTC()
	}
	record, err := readEnrollment(ctx, tx, input.TokenHash)
	if err != nil {
		return agentjoin.Result{}, err
	}
	if record.RevokedAt.Valid || !now.Before(record.ExpiresAt) || !input.Secure && !record.AllowHTTP {
		return agentjoin.Result{}, ErrEnrollmentUnauthorized
	}
	attempt := enrollmentAttempt{record: &record, input: input, now: now}
	if record.ConsumedAt.Valid {
		err = resumeEnrollment(ctx, tx, attempt)
	} else {
		err = activateEnrollment(ctx, tx, attempt)
	}
	if err != nil {
		return agentjoin.Result{}, err
	}
	if !record.ConsumedAt.Valid {
		if err := guardAlertScopeMutation(ctx, tx); err != nil {
			return agentjoin.Result{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return agentjoin.Result{}, fmt.Errorf("commit node enrollment: %w", err)
	}
	return enrollmentResult(record, input.Request.AgentToken), nil
}

func prepareEnrollmentInput(input EnrollmentConsumeInput) (EnrollmentConsumeInput, error) {
	if err := input.Request.Validate(); err != nil {
		return input, err
	}
	if len(input.TokenHash) != sha256.Size {
		return input, ErrEnrollmentUnauthorized
	}
	address, err := netip.ParseAddr(input.SourceIP)
	if err != nil || address.Zone() != "" {
		return input, ErrEnrollmentSource
	}
	input.SourceIP = address.Unmap().String()
	if input.Request.AdvertisedHost == "" {
		input.Request.AdvertisedHost = input.SourceIP
	}
	input.Request.NodeName = strings.TrimSpace(input.Request.NodeName)
	return input, nil
}

func readEnrollment(ctx context.Context, db schemaExecutor, hash []byte) (enrollmentRecord, error) {
	var record enrollmentRecord
	err := db.QueryRowContext(ctx, `SELECT id, server_url, creator_id, allow_http, expires_at,
		revoked_at, consumed_at, credential_created_at, request_id, node_id, agent_token_hash, node_name, advertised_host, ssh_port
		FROM agent_enrollment_tokens WHERE token_hash = ?`, hash).Scan(
		&record.ID, &record.ServerURL, &record.CreatorID, &record.AllowHTTP, &record.ExpiresAt,
		&record.RevokedAt, &record.ConsumedAt, &record.CredentialCreatedAt, &record.RequestID, &record.NodeID,
		&record.AgentTokenHash, &record.NodeName, &record.Host, &record.SSHPort)
	if errors.Is(err, sql.ErrNoRows) {
		return enrollmentRecord{}, ErrEnrollmentUnauthorized
	}
	if err != nil {
		return enrollmentRecord{}, fmt.Errorf("read enrollment credential: %w", err)
	}
	return record, nil
}

func resumeEnrollment(ctx context.Context, db schemaExecutor, attempt enrollmentAttempt) error {
	record, request := attempt.record, attempt.input.Request
	if request.RequestID != record.RequestID || !agentcredential.Matches(record.AgentTokenHash, request.AgentToken) {
		return ErrEnrollmentUnauthorized
	}
	if !record.NodeID.Valid || !record.CredentialCreatedAt.Valid {
		return ErrEnrollmentIdentityUnavailable
	}
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE id = ? AND agent_token_hash = ?
		AND agent_token_created_at = ? AND agent_token_revoked_at IS NULL`,
		record.NodeID.String, record.AgentTokenHash, record.CredentialCreatedAt.Time).Scan(&count)
	if err != nil {
		return fmt.Errorf("check enrolled node credential: %w", err)
	}
	if count != 1 {
		return ErrEnrollmentIdentityUnavailable
	}
	return nil
}

func activateEnrollment(ctx context.Context, db schemaExecutor, attempt enrollmentAttempt) error {
	node, err := findEnrollmentIdentity(ctx, db, attempt.input.Request)
	if err != nil {
		return err
	}
	if node == nil {
		node, err = createEnrollmentNode(ctx, db, attempt)
		if err != nil {
			return err
		}
	}
	if err := applyAcceleratorEnrollmentPolicy(ctx, db, node.ID); err != nil {
		return err
	}
	if err := rememberEnrollmentIdentity(ctx, db, *node); err != nil {
		return err
	}
	input, record := attempt.input, attempt.record
	_, err = db.ExecContext(ctx, `UPDATE agent_enrollment_tokens SET consumed_at = ?, credential_created_at = ?, request_id = ?,
		node_id = ?, agent_token_hash = ?, node_name = ?, advertised_host = ?, ssh_port = ?, consumed_ip = ? WHERE id = ?`,
		attempt.now, node.CreatedAt.Time, input.Request.RequestID, node.ID, node.Hash,
		node.Name, node.Host, node.Port, input.SourceIP, record.ID)
	if err != nil {
		return fmt.Errorf("consume enrollment credential: %w", err)
	}
	if err := recordEnrollmentAudit(ctx, db, enrollmentAudit{record: *record, input: input, nodeID: node.ID, now: attempt.now}); err != nil {
		return err
	}
	record.NodeID = sql.NullString{String: node.ID, Valid: true}
	record.NodeName, record.Host, record.SSHPort = node.Name, node.Host, node.Port
	return nil
}

func enrollmentResult(record enrollmentRecord, token string) agentjoin.Result {
	return agentjoin.Result{NodeID: record.NodeID.String, Config: agentjoin.Config{
		ServerURL: record.ServerURL, AgentToken: token, NodeName: record.NodeName,
		AdvertisedHost: record.Host, SSHPort: record.SSHPort, ReportInterval: agentjoin.DefaultReportInterval,
	}}
}
