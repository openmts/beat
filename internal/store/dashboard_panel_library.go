package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

var ErrLibraryPanelNotFound = errors.New("dashboard library panel not found")

var ErrLibraryPanelLimit = errors.New("dashboard library panel limit reached")

const dashboardLibraryPanelColumns = `id, owner_user_id, name, description, panel_json, created_at, updated_at`

type DashboardPanelLibraryStore struct{ db *sql.DB }

type LibraryPanelWrite struct {
	Value model.DashboardLibraryPanel
	Actor DashboardActor
}

func NewDashboardPanelLibraryStore(db *sql.DB) *DashboardPanelLibraryStore {
	return &DashboardPanelLibraryStore{db: db}
}

func (store *DashboardPanelLibraryStore) List(ctx context.Context, userID string) ([]model.DashboardLibraryPanel, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+dashboardLibraryPanelColumns+` FROM dashboard_panel_library
		WHERE owner_user_id=? ORDER BY updated_at DESC, id LIMIT ?`, userID, model.MaxLibraryPanelsPerOwner)
	if err != nil {
		return nil, fmt.Errorf("list dashboard library panels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := []model.DashboardLibraryPanel{}
	for rows.Next() {
		value, err := scanLibraryPanel(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func (store *DashboardPanelLibraryStore) Get(ctx context.Context, id string) (*model.DashboardLibraryPanel, error) {
	return scanLibraryPanel(store.db.QueryRowContext(ctx,
		"SELECT "+dashboardLibraryPanelColumns+" FROM dashboard_panel_library WHERE id=?", id))
}

func scanLibraryPanel(scanner rowScanner) (*model.DashboardLibraryPanel, error) {
	var value model.DashboardLibraryPanel
	var owner, encoded string
	err := scanner.Scan(&value.ID, &owner, &value.Name, &value.Description, &encoded, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLibraryPanelNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(encoded), &value.Panel); err != nil {
		return nil, fmt.Errorf("decode dashboard library panel: %w", err)
	}
	if err := model.ValidateLibraryPanel(value); err != nil {
		return nil, err
	}
	return &value, nil
}

func encodeLibraryPanel(panel model.DashboardPanel) (string, error) {
	encoded, err := json.Marshal(panel)
	if err != nil {
		return "", err
	}
	if len(encoded) > model.MaxLibraryPanelBytes {
		return "", fmt.Errorf("%w: panel exceeds 16 KiB", model.ErrInvalidLibraryPanel)
	}
	return string(encoded), nil
}
