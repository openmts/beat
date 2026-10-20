package store

import (
	"context"
	"fmt"
)

// KnownSecurityFingerprint 判断该管理员在回看窗口内是否已使用过该指纹。
func (store *AdminStore) KnownSecurityFingerprint(
	ctx context.Context, actorUserID, epoch, kind string, fingerprint []byte, since int64,
) (bool, error) {
	var found int
	err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_login_fingerprints
		WHERE actor_user_id=? AND epoch=? AND kind=? AND fingerprint=? AND last_seen_at >= ?`,
		actorUserID, epoch, kind, fingerprint, since).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("read security fingerprint: %w", err)
	}
	return found > 0, nil
}

// CountSecurityFingerprints 返回已知来源/设备指纹行数，供诊断使用。
func (store *AdminStore) CountSecurityFingerprints(ctx context.Context) (int64, error) {
	var total int64
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM security_login_fingerprints").Scan(&total); err != nil {
		return 0, fmt.Errorf("count security fingerprints: %w", err)
	}
	return total, nil
}

// MarkSecurityEventRouting 记录通知队列回执确认的路由状态。
func (store *AdminStore) MarkSecurityEventRouting(ctx context.Context, id, routing string) error {
	if !securityRoutingValueValid(routing) {
		return ErrSecurityEventInvalid
	}
	result, err := store.db.ExecContext(ctx, "UPDATE security_events SET routing_state=? WHERE id=?", routing, id)
	if err := securityRowsAffected(result, err, "mark security event routing"); err != nil {
		return err
	}
	return nil
}

// SecurityDiagnostics 汇总登录安全运行时规模，用于指标与诊断端点。
type SecurityDiagnostics struct {
	Thresholds   int64
	Fingerprints int64
}
