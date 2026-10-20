package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type dashboardMutation struct {
	ctx         context.Context
	transaction *sql.Tx
	write       DashboardWrite
	authority   dashboardAuthority
	encoded     string
	existing    *dashboardRecord
	// summary 是本次修订的版本备注，写进 dashboard_versions 供版本历史展示。
	summary string
	// restore 标记本次修订来自「恢复历史版本」，仅影响审计动作名。
	restore bool
}

func (store *DashboardStore) Save(ctx context.Context, write DashboardWrite) (*model.MetricDashboard, error) {
	if err := model.ValidateDashboardVersionSummary(write.Summary); err != nil {
		return nil, err
	}
	encoded, err := encodeDashboard(write.Value.Definition)
	if err != nil {
		return nil, err
	}
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	mutation := dashboardMutation{ctx: ctx, transaction: transaction, write: write, encoded: encoded, summary: write.Summary}
	if err := mutation.prepare(); err != nil {
		return nil, err
	}
	if err := mutation.persist(); err != nil {
		return nil, err
	}
	if err := mutation.audit("metrics.dashboard.save"); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return &mutation.write.Value, nil
}

func (mutation *dashboardMutation) prepare() error {
	authority, err := dashboardActorAuthority(mutation.ctx, mutation.transaction, mutation.write.Actor)
	if err != nil {
		return err
	}
	mutation.authority = authority
	if err := authority.allow(mutation.write.Value); err != nil {
		return err
	}
	existing, err := readDashboard(mutation.ctx, mutation.transaction, mutation.write.Value.ID)
	if err != nil {
		return err
	}
	if err := validateDashboardIdentity(mutation.write, existing); err != nil {
		return err
	}
	mutation.existing = existing
	if existing == nil && mutation.write.Value.Scope == "personal" {
		return mutation.checkPersonalLimit()
	}
	return nil
}

func (mutation *dashboardMutation) checkPersonalLimit() error {
	var count int
	err := mutation.transaction.QueryRowContext(mutation.ctx,
		"SELECT COUNT(*) FROM metric_dashboards WHERE scope='personal' AND owner_user_id=?", mutation.write.Actor.UserID).Scan(&count)
	if err != nil {
		return err
	}
	if count >= model.MaxPersonalDashboards {
		return ErrDashboardLimit
	}
	return nil
}

func (mutation *dashboardMutation) persist() error {
	value := &mutation.write.Value
	value.Revision = mutation.write.ExpectedRevision + 1
	value.UpdatedAt, value.UpdatedBy, value.CreatedAt = mutation.authority.now, mutation.write.Actor.UserID, mutation.authority.now
	var previous any
	if mutation.existing != nil {
		previous, value.HasPrevious, value.CreatedAt = mutation.existing.current, true, mutation.existing.value.CreatedAt
	}
	result, err := mutation.transaction.ExecContext(mutation.ctx, `INSERT INTO metric_dashboards
		(id,scope,owner_user_id,node_id,revision,definition_json,previous_definition_json,created_at,updated_at,updated_by)
		VALUES(?,?,NULLIF(?,''),NULLIF(?,''),?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
		revision=excluded.revision,definition_json=excluded.definition_json,
		previous_definition_json=excluded.previous_definition_json,updated_at=excluded.updated_at,updated_by=excluded.updated_by
		WHERE metric_dashboards.revision=?`, value.ID, value.Scope, value.OwnerUserID, value.NodeID,
		value.Revision, mutation.encoded, previous, value.CreatedAt, value.UpdatedAt, value.UpdatedBy, mutation.write.ExpectedRevision)
	if err := requireDashboardWrite(result, err); err != nil {
		return err
	}
	return mutation.recordVersion()
}

func (mutation *dashboardMutation) audit(action string) error {
	actor, value := mutation.write.Actor, mutation.write.Value
	detail, err := json.Marshal(struct {
		Scope    string `json:"scope"`
		Revision int    `json:"revision"`
	}{value.Scope, value.Revision})
	if err != nil {
		return err
	}
	return insertAuditEvent(mutation.ctx, mutation.transaction, &model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: dashboardActorField(actor.RequestID, 128), ActorID: actor.UserID,
		ActorUsername: mutation.authority.username, SessionPrefix: mutation.authority.sessionPrefix,
		Action: action, ResourceType: "metric_dashboard", ResourceID: value.ID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail),
		IPAddress: dashboardActorField(actor.IPAddress, 128), UserAgent: dashboardActorField(actor.UserAgent, 512),
		CreatedAt: mutation.authority.now,
	})
}

func encodeDashboard(definition model.DashboardDefinition) (string, error) {
	if err := model.ValidateDashboard(definition); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	if len(encoded) > model.MaxDashboardBytes {
		return "", fmt.Errorf("%w: definition exceeds 64 KiB", model.ErrInvalidDashboard)
	}
	return string(encoded), nil
}

func requireDashboardWrite(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("metric dashboard write affected no row")
	}
	return nil
}
