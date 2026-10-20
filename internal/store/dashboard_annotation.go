package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

var ErrAnnotationNotFound = errors.New("dashboard annotation not found")

var ErrAnnotationLimit = errors.New("dashboard annotation limit reached")

const dashboardAnnotationColumns = `id, dashboard_id, panel_id, title, text, tags_json, color,
	visibility, from_ts, to_ts, created_at, updated_at, created_by, updated_by`

type DashboardAnnotationStore struct{ db *sql.DB }

type AnnotationFilter struct {
	DashboardID string
	From        time.Time
	To          time.Time
	UserID      string
}

type AnnotationWrite struct {
	Value model.DashboardAnnotation
	Actor DashboardActor
}

func NewDashboardAnnotationStore(db *sql.DB) *DashboardAnnotationStore {
	return &DashboardAnnotationStore{db: db}
}

// List 返回看板时间窗口内的注释；私有注释只对创建者本人可见。
func (store *DashboardAnnotationStore) List(ctx context.Context, filter AnnotationFilter) ([]model.DashboardAnnotation, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+dashboardAnnotationColumns+` FROM dashboard_annotations
		WHERE dashboard_id=? AND to_ts>=? AND from_ts<=? AND (visibility='public' OR created_by=?)
		ORDER BY from_ts, id LIMIT ?`, filter.DashboardID, filter.From, filter.To, filter.UserID, model.MaxAnnotationsPerDashboard)
	if err != nil {
		return nil, fmt.Errorf("list dashboard annotations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := []model.DashboardAnnotation{}
	for rows.Next() {
		value, err := scanDashboardAnnotation(rows)
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

func (store *DashboardAnnotationStore) Get(ctx context.Context, id string) (*model.DashboardAnnotation, error) {
	value, err := scanDashboardAnnotation(store.db.QueryRowContext(ctx, "SELECT "+dashboardAnnotationColumns+" FROM dashboard_annotations WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAnnotationNotFound
	}
	return value, err
}

func scanDashboardAnnotation(scanner rowScanner) (*model.DashboardAnnotation, error) {
	var value model.DashboardAnnotation
	var tags string
	err := scanner.Scan(&value.ID, &value.DashboardID, &value.PanelID, &value.Title, &value.Text, &tags,
		&value.Color, &value.Visibility, &value.From, &value.To, &value.CreatedAt, &value.UpdatedAt,
		&value.CreatedBy, &value.UpdatedBy)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(tags), &value.Tags); err != nil {
		return nil, fmt.Errorf("decode dashboard annotation tags: %w", err)
	}
	if value.Tags == nil {
		value.Tags = []string{}
	}
	if err := model.ValidateDashboardAnnotation(value); err != nil {
		return nil, err
	}
	return &value, nil
}

// annotationVisibleTo 决定非创建者能否读取或修改该注释。
func annotationVisibleTo(value model.DashboardAnnotation, actor DashboardActor, authority dashboardAuthority) bool {
	if value.CreatedBy == actor.UserID {
		return true
	}
	return authority.role == model.AdminRoleOwner
}
