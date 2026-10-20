package store

import (
	"context"
	"fmt"
	"time"
)

// LoginThresholdKey 定位一个来源或用户名阈值键，不包含具体窗口。
type LoginThresholdKey struct {
	KeyKind string
	KeyHash []byte
}

// SecurityLockActive 返回任一键当前是否处于持久化锁定窗口内。
func (store *AdminStore) SecurityLockActive(
	ctx context.Context, keys []LoginThresholdKey, now time.Time,
) (bool, error) {
	for _, key := range keys {
		var locked int
		err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_login_thresholds
			WHERE key_kind=? AND key_hash=? AND lock_expires_at > ?`,
			key.KeyKind, key.KeyHash, now.UnixMilli()).Scan(&locked)
		if err != nil {
			return false, fmt.Errorf("read security lock state: %w", err)
		}
		if locked > 0 {
			return true, nil
		}
	}
	return false, nil
}

// ClearSecurityThresholds 在成功登录后清理该来源与用户名的失败窗口。
func (store *AdminStore) ClearSecurityThresholds(ctx context.Context, keys []LoginThresholdKey) error {
	for _, key := range keys {
		if _, err := store.db.ExecContext(ctx, "DELETE FROM security_login_thresholds WHERE key_kind=? AND key_hash=?",
			key.KeyKind, key.KeyHash); err != nil {
			return fmt.Errorf("clear security threshold: %w", err)
		}
	}
	return nil
}

// CountSecurityThresholds 返回持久化阈值行数，供指标与诊断使用。
func (store *AdminStore) CountSecurityThresholds(ctx context.Context) (int64, error) {
	var total int64
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM security_login_thresholds").Scan(&total); err != nil {
		return 0, fmt.Errorf("count security thresholds: %w", err)
	}
	return total, nil
}
