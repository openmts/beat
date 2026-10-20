package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (store *DashboardPanelLibraryStore) Create(ctx context.Context, write LibraryPanelWrite) (*model.DashboardLibraryPanel, error) {
	write.Value.ID = uuid.NewString()
	return store.save(ctx, write, "metrics.library_panel.create", true)
}

func (store *DashboardPanelLibraryStore) Update(ctx context.Context, write LibraryPanelWrite) (*model.DashboardLibraryPanel, error) {
	return store.save(ctx, write, "metrics.library_panel.update", false)
}

func (store *DashboardPanelLibraryStore) save(ctx context.Context, write LibraryPanelWrite, action string, create bool) (*model.DashboardLibraryPanel, error) {
	if err := model.ValidateLibraryPanel(write.Value); err != nil {
		return nil, err
	}
	encoded, err := encodeLibraryPanel(write.Value.Panel)
	if err != nil {
		return nil, err
	}
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	authority, err := dashboardActorAuthority(ctx, transaction, write.Actor)
	if err != nil {
		return nil, err
	}
	if create {
		if err := libraryPanelLimitAllows(ctx, transaction, write.Actor.UserID); err != nil {
			return nil, err
		}
		write.Value.CreatedAt = authority.now
	} else {
		existing, err := ownedLibraryPanel(ctx, transaction, write.Value.ID, write.Actor.UserID)
		if err != nil {
			return nil, err
		}
		write.Value.CreatedAt = existing.CreatedAt
	}
	write.Value.UpdatedAt = authority.now
	if _, err := transaction.ExecContext(ctx, `INSERT INTO dashboard_panel_library
		(id,owner_user_id,name,description,panel_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,
		panel_json=excluded.panel_json,updated_at=excluded.updated_at`,
		write.Value.ID, write.Actor.UserID, write.Value.Name, write.Value.Description, encoded,
		write.Value.CreatedAt, write.Value.UpdatedAt); err != nil {
		return nil, fmt.Errorf("write dashboard library panel: %w", err)
	}
	if err := libraryPanelAudit(ctx, transaction, authority, write, action); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return &write.Value, nil
}

func (store *DashboardPanelLibraryStore) Delete(ctx context.Context, id string, actor DashboardActor) error {
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	authority, err := dashboardActorAuthority(ctx, transaction, actor)
	if err != nil {
		return err
	}
	existing, err := ownedLibraryPanel(ctx, transaction, id, actor.UserID)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM dashboard_panel_library WHERE id=?", id); err != nil {
		return fmt.Errorf("delete dashboard library panel: %w", err)
	}
	write := LibraryPanelWrite{Value: *existing, Actor: actor}
	if err := libraryPanelAudit(ctx, transaction, authority, write, "metrics.library_panel.delete"); err != nil {
		return err
	}
	return transaction.Commit()
}

func ownedLibraryPanel(ctx context.Context, database schemaExecutor, id, userID string) (*model.DashboardLibraryPanel, error) {
	value, err := scanLibraryPanel(database.QueryRowContext(ctx,
		"SELECT "+dashboardLibraryPanelColumns+" FROM dashboard_panel_library WHERE id=? AND owner_user_id=?", id, userID))
	if err != nil {
		return nil, ErrLibraryPanelNotFound
	}
	return value, nil
}

func libraryPanelLimitAllows(ctx context.Context, database schemaExecutor, userID string) error {
	var count int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM dashboard_panel_library WHERE owner_user_id=?", userID).Scan(&count); err != nil {
		return err
	}
	if count >= model.MaxLibraryPanelsPerOwner {
		return ErrLibraryPanelLimit
	}
	return nil
}

func libraryPanelAudit(ctx context.Context, database schemaExecutor, authority dashboardAuthority, write LibraryPanelWrite, action string) error {
	detail, err := json.Marshal(struct {
		Name string `json:"name"`
	}{write.Value.Name})
	if err != nil {
		return err
	}
	return insertAuditEvent(ctx, database, &model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: dashboardActorField(write.Actor.RequestID, 128),
		ActorID: write.Actor.UserID, ActorUsername: authority.username,
		SessionPrefix: authority.sessionPrefix, Action: action, ResourceType: "dashboard_library_panel",
		ResourceID: write.Value.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail),
		IPAddress: dashboardActorField(write.Actor.IPAddress, 128),
		UserAgent: dashboardActorField(write.Actor.UserAgent, 512), CreatedAt: authority.now,
	})
}
