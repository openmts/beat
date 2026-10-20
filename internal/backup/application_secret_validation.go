package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/applicationsecrets"
)

func validateV9Secrets(ctx context.Context, sqlitePath, keyPath string) (handled bool, resultErr error) {
	database, err := openArchivedSQLite(sqlitePath)
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var version sql.NullInt64
	if err := database.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		if isNoSuchTable(err) {
			return false, nil
		}
		return false, err
	}
	const lifecycleSchemaVersion = 9
	if !version.Valid || version.Int64 < lifecycleSchemaVersion {
		return false, nil
	}
	if err := applicationsecrets.ValidateSnapshot(ctx, sqlitePath, keyPath); err != nil {
		// 归档校验的失败原因会进服务端日志；补上上下文才能区分“归档密钥不对”和“归档本身损坏”。
		return true, fmt.Errorf("validate archived application secrets with the archived administrator data key: %w", err)
	}
	return true, nil
}
