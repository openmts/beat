package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

var (
	ErrDashboardNotFound  = errors.New("metric dashboard not found")
	ErrDashboardConflict  = errors.New("metric dashboard changed; reload before saving")
	ErrDashboardForbidden = errors.New("metric dashboard permission denied")
	ErrDashboardLimit     = errors.New("personal dashboard limit reached")
)

const dashboardColumns = `id, scope, COALESCE(owner_user_id, ''), COALESCE(node_id, ''),
	revision, definition_json, COALESCE(previous_definition_json, ''), created_at, updated_at, updated_by`

type DashboardStore struct{ db *sql.DB }

type DashboardActor struct {
	UserID, SessionID, RequestID, IPAddress, UserAgent string
}

type DashboardWrite struct {
	Value            model.MetricDashboard
	ExpectedRevision int
	Actor            DashboardActor
	// Summary 是可选的版本备注，随本次修订写进版本历史。
	Summary string
}

type DashboardDelete struct {
	ID               string
	ExpectedRevision int
	Actor            DashboardActor
}

type dashboardRecord struct {
	value             model.MetricDashboard
	current, previous string
}

func NewDashboardStore(db *sql.DB) *DashboardStore { return &DashboardStore{db: db} }

// DB 暴露底层连接，供看板协作面（注释/快照/面板库/面板告警）共享同一存储。
func (store *DashboardStore) DB() *sql.DB { return store.db }

func (store *DashboardStore) Get(ctx context.Context, id string) (*model.MetricDashboard, error) {
	record, err := readDashboard(ctx, store.db, id)
	if err != nil || record == nil {
		return nil, err
	}
	return &record.value, nil
}

func (store *DashboardStore) Previous(ctx context.Context, id string) (*model.DashboardDefinition, error) {
	record, err := readDashboard(ctx, store.db, id)
	if err != nil {
		return nil, err
	}
	if record == nil || record.previous == "" {
		return nil, ErrDashboardNotFound
	}
	var definition model.DashboardDefinition
	if err := json.Unmarshal([]byte(record.previous), &definition); err != nil {
		return nil, fmt.Errorf("decode previous metric dashboard: %w", err)
	}
	if err := model.ValidateDashboard(definition); err != nil {
		return nil, err
	}
	return &definition, nil
}

func (store *DashboardStore) List(ctx context.Context, userID string) ([]model.MetricDashboard, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+dashboardColumns+` FROM metric_dashboards
		WHERE scope IN ('public','admin') OR (scope='personal' AND owner_user_id=?)
		ORDER BY scope,id LIMIT ?`, userID, model.MaxPersonalDashboards+2)
	if err != nil {
		return nil, fmt.Errorf("list metric dashboards: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := []model.MetricDashboard{}
	for rows.Next() {
		record, err := scanDashboard(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, record.value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func readDashboard(ctx context.Context, database schemaExecutor, id string) (*dashboardRecord, error) {
	return scanDashboard(database.QueryRowContext(ctx, "SELECT "+dashboardColumns+" FROM metric_dashboards WHERE id=?", id))
}

func scanDashboard(scanner rowScanner) (*dashboardRecord, error) {
	var record dashboardRecord
	value := &record.value
	err := scanner.Scan(&value.ID, &value.Scope, &value.OwnerUserID, &value.NodeID,
		&value.Revision, &record.current, &record.previous, &value.CreatedAt, &value.UpdatedAt, &value.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read metric dashboard: %w", err)
	}
	if err := json.Unmarshal([]byte(record.current), &value.Definition); err != nil {
		return nil, fmt.Errorf("decode metric dashboard: %w", err)
	}
	if err := model.ValidateDashboard(value.Definition); err != nil {
		return nil, err
	}
	value.HasPrevious = record.previous != ""
	return &record, nil
}
