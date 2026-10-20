package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

var ErrMetadataForbidden = errors.New("current administrator authority is required")

var ErrMetadataRecentAuth = errors.New("recent administrator authentication is required")

// MetadataActor 仅由已认证的管理HTTP边界设置，内部采集/初始化不携带它。
type MetadataActor struct {
	DashboardActor
	Bootstrap bool
}

type metadataActorKey struct{}

func WithMetadataActor(ctx context.Context, actor MetadataActor) context.Context {
	return context.WithValue(ctx, metadataActorKey{}, actor)
}

type metadataMutation struct {
	ctx       context.Context
	tx        *sql.Tx
	actor     *MetadataActor
	authority dashboardAuthority
	recent    bool
}

type metadataEvidence struct {
	Action, Type, ID, Name string
	Fields                 []string
	Count                  int
}

func beginMetadataMutation(ctx context.Context, tx *sql.Tx, recent bool) (metadataMutation, error) {
	mutation := metadataMutation{ctx: ctx, tx: tx, recent: recent}
	actor, exists := ctx.Value(metadataActorKey{}).(MetadataActor)
	if !exists {
		return mutation, nil
	}
	mutation.actor = &actor
	return mutation, mutation.authorize()
}

func (m *metadataMutation) authorize() error {
	if m.actor == nil {
		return nil
	}
	if m.actor.Bootstrap {
		var count int
		if err := m.tx.QueryRowContext(m.ctx, "SELECT COUNT(*) FROM admin_users").Scan(&count); err != nil {
			return err
		}
		if count != 0 || m.recent {
			return ErrMetadataForbidden
		}
		m.authority = dashboardAuthority{username: "bootstrap_authority", now: model.NowUTC()}
		return nil
	}
	authority, err := dashboardActorAuthority(m.ctx, m.tx, m.actor.DashboardActor)
	if errors.Is(err, ErrDashboardForbidden) {
		return ErrMetadataForbidden
	}
	if err != nil {
		return err
	}
	if authority.role != model.AdminRoleOwner && authority.role != model.AdminRoleAdmin {
		return ErrMetadataForbidden
	}
	if m.recent && (!authority.reauthenticated.Valid || !authority.now.Before(authority.reauthenticated.Time)) {
		return ErrMetadataRecentAuth
	}
	m.authority = authority
	return nil
}

// finish 记录一次成功的管理变更；没有审计主体时保持历史的无审计行为。
func (m metadataMutation) finish(evidence metadataEvidence) error {
	return m.record(evidence, model.AuditOutcomeSuccess)
}

// reject 记录一次被拒绝的管理变更；它只登记原因分类与字段名，绝不登记被拒的取值。
func (m metadataMutation) reject(evidence metadataEvidence) error {
	return m.record(evidence, model.AuditOutcomeFailure)
}

func (m metadataMutation) record(evidence metadataEvidence, outcome string) error {
	if m.actor == nil {
		return nil
	}
	if err := m.authorize(); err != nil {
		return err
	}
	details, err := json.Marshal(struct {
		Name   string   `json:"name,omitempty"`
		Fields []string `json:"changed_fields,omitempty"`
		Count  int      `json:"target_count,omitempty"`
	}{Name: dashboardActorField(evidence.Name, 256), Fields: evidence.Fields, Count: evidence.Count})
	if err != nil {
		return err
	}
	actor := m.actor
	event := model.AdminAuditEvent{ID: uuid.NewString(), RequestID: dashboardActorField(actor.RequestID, 128), ActorID: actor.UserID,
		ActorUsername: m.authority.username, SessionPrefix: m.authority.sessionPrefix, Action: evidence.Action, ResourceType: evidence.Type, ResourceID: evidence.ID,
		Outcome: outcome, DetailJSON: string(details), IPAddress: dashboardActorField(actor.IPAddress, 128), UserAgent: dashboardActorField(actor.UserAgent, 512), CreatedAt: m.authority.now}
	if event.RequestID == "" {
		event.RequestID = uuid.NewString()
	}
	if err := insertAuditEvent(m.ctx, m.tx, &event); err != nil {
		return err
	}
	if err := m.verifyAudit(event); err != nil {
		return err
	}
	return m.authorize()
}

func (m metadataMutation) verifyAudit(event model.AdminAuditEvent) error {
	var count int
	err := m.tx.QueryRowContext(m.ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE id=? AND request_id=? AND COALESCE(actor_id,'')=? AND actor_username=? AND action=? AND resource_type=? AND resource_id=? AND outcome=? AND detail_json=? AND session_prefix=?`, event.ID, event.RequestID, event.ActorID, event.ActorUsername, event.Action, event.ResourceType, event.ResourceID, event.Outcome, event.DetailJSON, event.SessionPrefix).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("metadata audit was not persisted")
	}
	return nil
}
