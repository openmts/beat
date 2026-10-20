package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrInvalidBackupQuery 表示游标、状态过滤或 limit 不合法；调用方应返回 400。
var ErrInvalidBackupQuery = errors.New("invalid backup query")

const (
	// BackupDefaultPage 与 BackupMaximumPage 约束备份历史的每页上限。
	BackupDefaultPage = 50
	BackupMaximumPage = 200

	backupMaximumCursorBytes = 512
	backupCursorVersion      = 1
)

// BackupQuery 是备份历史的服务端筛选与游标分页输入。
type BackupQuery struct {
	State  string
	Cursor string
	Limit  int
}

// backupCursor 绑定状态过滤器，并按纳秒精度携带 (created_at,id) 位置，
// 避免毫秒截断在同一时间窗内静默跳过记录。
type backupCursor struct {
	State   string `json:"state,omitempty"`
	At      string `json:"at"`
	ID      string `json:"id"`
	Version int    `json:"v"`
}

// ListBackupsPage 用不透明的 (created_at,id) 游标分页，不再整表下发。
func (store *BackupStore) ListBackupsPage(ctx context.Context, query BackupQuery) (model.BackupPage, error) {
	statement, args, limit, err := backupPageQuery(query)
	if err != nil {
		return model.BackupPage{}, err
	}
	rows, err := store.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return model.BackupPage{}, fmt.Errorf("list backup records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := model.BackupPage{Items: []model.BackupRecord{}}
	for rows.Next() {
		record, scanErr := scanBackup(rows)
		if scanErr != nil {
			return model.BackupPage{}, fmt.Errorf("scan backup record: %w", scanErr)
		}
		page.Items = append(page.Items, record)
	}
	if err := rows.Err(); err != nil {
		return model.BackupPage{}, fmt.Errorf("iterate backup records: %w", err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor, err = encodeBackupCursor(query, page.Items[len(page.Items)-1])
	}
	return page, err
}

func backupPageQuery(query BackupQuery) (string, []any, int, error) {
	limit, err := backupQueryLimit(query.Limit)
	if err != nil {
		return "", nil, 0, err
	}
	if query.State != "" && !validBackupState(query.State) {
		return "", nil, 0, ErrInvalidBackupQuery
	}
	cursor, err := decodeBackupCursor(query.Cursor)
	if err != nil {
		return "", nil, 0, err
	}
	filters, args := []string{"1=1"}, []any{}
	if query.State != "" {
		filters = append(filters, "state=?")
		args = append(args, query.State)
	}
	if query.Cursor != "" {
		if cursor.State != query.State {
			return "", nil, 0, ErrInvalidBackupQuery
		}
		at, parseErr := time.Parse(time.RFC3339Nano, cursor.At)
		if parseErr != nil {
			return "", nil, 0, ErrInvalidBackupQuery
		}
		filters = append(filters, "(created_at<? OR (created_at=? AND id<?))")
		at = at.UTC()
		args = append(args, at, at, cursor.ID)
	}
	statement := "SELECT " + backupColumns + " FROM admin_backups WHERE " +
		strings.Join(filters, " AND ") + " ORDER BY created_at DESC,id DESC LIMIT ?"
	return statement, append(args, limit+1), limit, nil
}

func validBackupState(state string) bool {
	switch state {
	case model.BackupStateRunning, model.BackupStateReady, model.BackupStateFailed,
		model.BackupStateValidated, model.BackupStateStaging, model.BackupStateStaged,
		model.BackupStateInterrupted, model.BackupStateMissing, model.BackupStateDeleting:
		return true
	default:
		return false
	}
}

func backupQueryLimit(limit int) (int, error) {
	if limit == 0 {
		return BackupDefaultPage, nil
	}
	if limit < 1 || limit > BackupMaximumPage {
		return 0, ErrInvalidBackupQuery
	}
	return limit, nil
}

func encodeBackupCursor(query BackupQuery, last model.BackupRecord) (string, error) {
	data, err := json.Marshal(backupCursor{
		State: query.State, At: last.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID: last.ID, Version: backupCursorVersion,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeBackupCursor(encoded string) (backupCursor, error) {
	var cursor backupCursor
	if encoded == "" {
		return cursor, nil
	}
	if len(encoded) > backupMaximumCursorBytes {
		return cursor, ErrInvalidBackupQuery
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return cursor, ErrInvalidBackupQuery
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		return cursor, ErrInvalidBackupQuery
	}
	if cursor.Version != backupCursorVersion || cursor.ID == "" || len(cursor.ID) > 128 || cursor.At == "" {
		return cursor, ErrInvalidBackupQuery
	}
	return cursor, nil
}
