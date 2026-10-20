package store

import (
	"context"
	"database/sql"

	"github.com/beat/backend/internal/alerteval"
)

const alertScopeSchemaVersion = 12

// v12登记前不启用高级规则限制；登记后的所有scope扩展必须在写事务内核验。
func guardAlertScopeMutation(ctx context.Context, tx *sql.Tx) error {
	var enabled bool
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='alert_rule_nodes'),COALESCE((SELECT MAX(version) FROM schema_migrations),0)").Scan(&enabled, &version); err != nil {
		return err
	}
	if !enabled {
		if version >= alertScopeSchemaVersion {
			return alerteval.ErrUnavailable
		}
		return nil
	}
	return ValidateAlertPairCapacityTx(ctx, tx)
}
