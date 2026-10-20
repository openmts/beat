package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (store *DashboardSnapshotStore) Create(ctx context.Context, write SnapshotWrite) (*model.DashboardSnapshot, error) {
	encoded, err := encodeSnapshotDefinition(write.Value.Definition)
	if err != nil {
		return nil, err
	}
	write.Value.ID = uuid.NewString()
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	authority, err := dashboardActorAuthority(ctx, transaction, write.Actor)
	if err != nil {
		return nil, err
	}
	write.Value.CreatedBy, write.Value.CreatedAt = write.Actor.UserID, authority.now
	if err := model.ValidateDashboardSnapshot(write.Value); err != nil {
		return nil, err
	}
	if err := snapshotLimitAllows(ctx, transaction, write.Actor.UserID); err != nil {
		return nil, err
	}
	token, err := newSnapshotToken(make([]byte, snapshotTokenBytes))
	if err != nil {
		return nil, err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO dashboard_snapshots
		(id,token,name,dashboard_id,revision,definition_json,expires_at,created_at,created_by)
		VALUES(?,?,?,?,?,?,?,?,?)`, write.Value.ID, token, write.Value.Name, write.Value.DashboardID,
		write.Value.Revision, encoded, write.Value.ExpiresAt, write.Value.CreatedAt, write.Value.CreatedBy); err != nil {
		return nil, fmt.Errorf("write dashboard snapshot: %w", err)
	}
	if err := snapshotAudit(ctx, transaction, authority, write.Actor, "metrics.snapshot.create", write.Value.ID, ""); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	write.Value.Token = token
	return &write.Value, nil
}

// Revoke 撤销分享链接；只有创建者本人或 owner 可以操作。
func (store *DashboardSnapshotStore) Revoke(ctx context.Context, id string, actor DashboardActor) error {
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	authority, err := dashboardActorAuthority(ctx, transaction, actor)
	if err != nil {
		return err
	}
	existing, err := scanDashboardSnapshot(transaction.QueryRowContext(ctx,
		"SELECT "+dashboardSnapshotColumns+" FROM dashboard_snapshots WHERE id=?", id))
	if err != nil {
		return err
	}
	if existing.CreatedBy != actor.UserID && authority.role != model.AdminRoleOwner {
		return ErrSnapshotNotFound
	}
	if existing.RevokedAt == nil {
		if _, err := transaction.ExecContext(ctx,
			"UPDATE dashboard_snapshots SET revoked_at=? WHERE id=? AND revoked_at IS NULL", authority.now, id); err != nil {
			return fmt.Errorf("revoke dashboard snapshot: %w", err)
		}
	}
	if err := snapshotAudit(ctx, transaction, authority, actor, "metrics.snapshot.revoke", id, existing.DashboardID); err != nil {
		return err
	}
	return transaction.Commit()
}

func snapshotLimitAllows(ctx context.Context, database schemaExecutor, userID string) error {
	var count int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM dashboard_snapshots WHERE created_by=? AND revoked_at IS NULL", userID).Scan(&count); err != nil {
		return err
	}
	if count >= model.MaxSnapshotsPerOwner {
		return ErrSnapshotLimit
	}
	return nil
}

func snapshotAudit(ctx context.Context, database schemaExecutor, authority dashboardAuthority, actor DashboardActor, action, id, dashboardID string) error {
	detail, err := json.Marshal(struct {
		DashboardID string `json:"dashboard_id,omitempty"`
	}{dashboardID})
	if err != nil {
		return err
	}
	return insertAuditEvent(ctx, database, &model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: dashboardActorField(actor.RequestID, 128), ActorID: actor.UserID,
		ActorUsername: authority.username, SessionPrefix: authority.sessionPrefix, Action: action,
		ResourceType: "dashboard_snapshot", ResourceID: id, Outcome: model.AuditOutcomeSuccess,
		DetailJSON: string(detail), IPAddress: dashboardActorField(actor.IPAddress, 128),
		UserAgent: dashboardActorField(actor.UserAgent, 512), CreatedAt: authority.now,
	})
}

func encodeSnapshotDefinition(definition model.DashboardDefinition) (string, error) {
	if err := model.ValidateDashboard(definition); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	if len(encoded) > model.MaximumSnapshotDefinition {
		return "", fmt.Errorf("%w: definition exceeds 64 KiB", model.ErrInvalidDashboardSnapshot)
	}
	return string(encoded), nil
}
