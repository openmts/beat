package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

var ErrSnapshotNotFound = errors.New("dashboard snapshot not found")

var ErrSnapshotLimit = errors.New("dashboard snapshot limit reached")

var ErrSnapshotExpired = errors.New("dashboard snapshot is no longer available")

const dashboardSnapshotColumns = `id, token, name, dashboard_id, revision, definition_json,
	expires_at, revoked_at, created_at, created_by`

// snapshotTokenBytes 决定 share token 的随机长度；hex 编码后是 URL 安全的 64 个字符。
const snapshotTokenBytes = 32

type DashboardSnapshotStore struct{ db *sql.DB }

type SnapshotWrite struct {
	Value model.DashboardSnapshot
	Actor DashboardActor
}

func NewDashboardSnapshotStore(db *sql.DB) *DashboardSnapshotStore {
	return &DashboardSnapshotStore{db: db}
}

// List 返回该创建者的快照，最新的在前。
func (store *DashboardSnapshotStore) List(ctx context.Context, userID string) ([]model.DashboardSnapshot, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+dashboardSnapshotColumns+` FROM dashboard_snapshots
		WHERE created_by=? ORDER BY created_at DESC, id LIMIT ?`, userID, model.MaxSnapshotsPerOwner)
	if err != nil {
		return nil, fmt.Errorf("list dashboard snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := []model.DashboardSnapshot{}
	for rows.Next() {
		value, err := scanDashboardSnapshot(rows)
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

// GetByToken 解析公开分享链接；已撤销或已过期的快照一律按不存在处理。
func (store *DashboardSnapshotStore) GetByToken(ctx context.Context, token string) (*model.DashboardSnapshot, error) {
	value, err := scanDashboardSnapshot(store.db.QueryRowContext(ctx,
		"SELECT "+dashboardSnapshotColumns+" FROM dashboard_snapshots WHERE token=?", token))
	if err != nil {
		return nil, err
	}
	if value.RevokedAt != nil || value.ExpiresAt != nil && !model.NowUTC().Before(*value.ExpiresAt) {
		return nil, ErrSnapshotNotFound
	}
	value.Token = ""
	return value, nil
}

func (store *DashboardSnapshotStore) Get(ctx context.Context, id string) (*model.DashboardSnapshot, error) {
	return scanDashboardSnapshot(store.db.QueryRowContext(ctx,
		"SELECT "+dashboardSnapshotColumns+" FROM dashboard_snapshots WHERE id=?", id))
}

func scanDashboardSnapshot(scanner rowScanner) (*model.DashboardSnapshot, error) {
	var value model.DashboardSnapshot
	var definition string
	var expires, revoked sql.NullTime
	err := scanner.Scan(&value.ID, &value.Token, &value.Name, &value.DashboardID, &value.Revision,
		&definition, &expires, &revoked, &value.CreatedAt, &value.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSnapshotNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(definition), &value.Definition); err != nil {
		return nil, fmt.Errorf("decode dashboard snapshot: %w", err)
	}
	if expires.Valid {
		at := expires.Time
		value.ExpiresAt = &at
	}
	if revoked.Valid {
		at := revoked.Time
		value.RevokedAt = &at
	}
	if err := model.ValidateDashboardSnapshot(value); err != nil {
		return nil, err
	}
	return &value, nil
}

func newSnapshotToken(source []byte) (string, error) {
	if _, err := rand.Read(source); err != nil {
		return "", fmt.Errorf("generate dashboard snapshot token: %w", err)
	}
	return hex.EncodeToString(source), nil
}
