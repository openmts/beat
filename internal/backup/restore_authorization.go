package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/beat/backend/internal/model"
)

// restoreAuthorized 判定 pending journal 是否可以替换活动库。
//
// 在线暂存必须由活动 SQLite 里 state=staged 且随机代次一致的记录授权；本机运维
// 恢复命令写入的 journal 带 offline 标记，用于数据库缺失的灾难恢复场景。
func restoreAuthorized(ctx context.Context, dbPath string, journal model.RestoreJournal) (bool, error) {
	if journal.Origin == model.RestoreOriginOffline {
		return true, nil
	}
	if journal.Generation == "" {
		return false, nil
	}
	location := url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&_query_only=1"}
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return false, fmt.Errorf("open live database for restore confirmation: %w", err)
	}
	defer func() { _ = database.Close() }()
	var state, generation string
	err = database.QueryRowContext(ctx,
		"SELECT state, restore_generation FROM admin_backups WHERE id = ?", journal.BackupID).
		Scan(&state, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		// 表结构缺失或活动库不可读时不确认；启动对账随后会回滚该 journal。
		return false, nil
	}
	return state == model.BackupStateStaged && generation == journal.Generation, nil
}
