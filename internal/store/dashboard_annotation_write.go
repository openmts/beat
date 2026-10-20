package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type annotationMutation struct {
	ctx       context.Context
	tx        *sql.Tx
	write     AnnotationWrite
	authority dashboardAuthority
	existing  *model.DashboardAnnotation
}

func (store *DashboardAnnotationStore) Create(ctx context.Context, write AnnotationWrite) (*model.DashboardAnnotation, error) {
	write.Value.ID = uuid.NewString()
	return store.apply(ctx, write, "metrics.annotation.create")
}

func (store *DashboardAnnotationStore) Update(ctx context.Context, write AnnotationWrite) (*model.DashboardAnnotation, error) {
	return store.apply(ctx, write, "metrics.annotation.update")
}

func (store *DashboardAnnotationStore) apply(ctx context.Context, write AnnotationWrite, action string) (*model.DashboardAnnotation, error) {
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	mutation := annotationMutation{ctx: ctx, tx: transaction, write: write}
	if err := mutation.prepare(action); err != nil {
		return nil, err
	}
	if err := mutation.persist(); err != nil {
		return nil, err
	}
	if err := mutation.audit(action); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return &mutation.write.Value, nil
}

func (mutation *annotationMutation) prepare(action string) error {
	authority, err := dashboardActorAuthority(mutation.ctx, mutation.tx, mutation.write.Actor)
	if err != nil {
		return err
	}
	mutation.authority = authority
	if action == "metrics.annotation.create" {
		return mutation.prepareCreate(authority)
	}
	existing, err := findAnnotation(mutation.ctx, mutation.tx, mutation.write.Value.ID)
	if err != nil {
		return err
	}
	if !annotationVisibleTo(*existing, mutation.write.Actor, authority) {
		return ErrAnnotationNotFound
	}
	mutation.existing = existing
	mutation.write.Value.CreatedAt, mutation.write.Value.CreatedBy = existing.CreatedAt, existing.CreatedBy
	return mutation.validate()
}

func (mutation *annotationMutation) prepareCreate(authority dashboardAuthority) error {
	if mutation.write.Value.CreatedBy == "" {
		mutation.write.Value.CreatedBy = mutation.write.Actor.UserID
	}
	mutation.write.Value.CreatedAt = authority.now
	if err := mutation.validate(); err != nil {
		return err
	}
	return mutation.checkLimit()
}

func (mutation *annotationMutation) validate() error {
	value := mutation.write.Value
	if err := model.ValidateDashboardAnnotation(value); err != nil {
		return err
	}
	if err := dashboardTargetExists(mutation.ctx, mutation.tx, value.DashboardID); err != nil {
		return err
	}
	if value.Visibility != model.AnnotationVisibilityPublic {
		return nil
	}
	if mutation.authority.role != model.AdminRoleOwner || !mutation.authority.reauthenticated.Valid {
		return ErrDashboardForbidden
	}
	if !mutation.authority.now.Before(mutation.authority.reauthenticated.Time) {
		return ErrDashboardForbidden
	}
	return nil
}

func (mutation *annotationMutation) checkLimit() error {
	var count int
	if err := mutation.tx.QueryRowContext(mutation.ctx,
		"SELECT COUNT(*) FROM dashboard_annotations WHERE dashboard_id=?", mutation.write.Value.DashboardID).Scan(&count); err != nil {
		return err
	}
	if count >= model.MaxAnnotationsPerDashboard {
		return ErrAnnotationLimit
	}
	return nil
}

func (mutation *annotationMutation) persist() error {
	value := mutation.write.Value
	value.UpdatedAt, value.UpdatedBy = mutation.authority.now, mutation.write.Actor.UserID
	tags, err := json.Marshal(value.Tags)
	if err != nil {
		return err
	}
	_, err = mutation.tx.ExecContext(mutation.ctx, `INSERT INTO dashboard_annotations
		(id,dashboard_id,panel_id,title,text,tags_json,color,visibility,from_ts,to_ts,created_at,updated_at,created_by,updated_by)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
		panel_id=excluded.panel_id,title=excluded.title,text=excluded.text,tags_json=excluded.tags_json,
		color=excluded.color,visibility=excluded.visibility,from_ts=excluded.from_ts,to_ts=excluded.to_ts,
		updated_at=excluded.updated_at,updated_by=excluded.updated_by`,
		value.ID, value.DashboardID, value.PanelID, value.Title, value.Text, string(tags), value.Color,
		value.Visibility, value.From, value.To, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy)
	if err != nil {
		return fmt.Errorf("write dashboard annotation: %w", err)
	}
	return nil
}

func (mutation *annotationMutation) audit(action string) error {
	value := mutation.write.Value
	detail, err := json.Marshal(struct {
		DashboardID string `json:"dashboard_id"`
		PanelID     string `json:"panel_id,omitempty"`
		Visibility  string `json:"visibility"`
	}{value.DashboardID, value.PanelID, value.Visibility})
	if err != nil {
		return err
	}
	return insertAuditEvent(mutation.ctx, mutation.tx, &model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: dashboardActorField(mutation.write.Actor.RequestID, 128),
		ActorID: mutation.write.Actor.UserID, ActorUsername: mutation.authority.username,
		SessionPrefix: mutation.authority.sessionPrefix, Action: action, ResourceType: "dashboard_annotation",
		ResourceID: value.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail),
		IPAddress: dashboardActorField(mutation.write.Actor.IPAddress, 128),
		UserAgent: dashboardActorField(mutation.write.Actor.UserAgent, 512), CreatedAt: mutation.authority.now,
	})
}

func (store *DashboardAnnotationStore) Delete(ctx context.Context, id string, actor DashboardActor) error {
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	authority, err := dashboardActorAuthority(ctx, transaction, actor)
	if err != nil {
		return err
	}
	existing, err := findAnnotation(ctx, transaction, id)
	if err != nil {
		return err
	}
	if !annotationVisibleTo(*existing, actor, authority) {
		return ErrAnnotationNotFound
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM dashboard_annotations WHERE id=?", id); err != nil {
		return fmt.Errorf("delete dashboard annotation: %w", err)
	}
	mutation := annotationMutation{ctx: ctx, tx: transaction, write: AnnotationWrite{Value: *existing, Actor: actor}, authority: authority}
	if err := mutation.audit("metrics.annotation.delete"); err != nil {
		return err
	}
	return transaction.Commit()
}

func findAnnotation(ctx context.Context, database schemaExecutor, id string) (*model.DashboardAnnotation, error) {
	value, err := scanDashboardAnnotation(database.QueryRowContext(ctx,
		"SELECT "+dashboardAnnotationColumns+" FROM dashboard_annotations WHERE id=?", id))
	if err != nil {
		return nil, ErrAnnotationNotFound
	}
	return value, nil
}

// dashboardTargetExists 允许内置模板与真实看板，拒绝写到不存在的看板上。
func dashboardTargetExists(ctx context.Context, database schemaExecutor, id string) error {
	if id == model.PublicDashboardID || id == model.AdminDashboardID || strings.HasPrefix(id, "node-") {
		return nil
	}
	var count int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM metric_dashboards WHERE id=?", id).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrDashboardNotFound
	}
	return nil
}
