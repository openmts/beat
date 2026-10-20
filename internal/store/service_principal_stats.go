package store

import (
	"context"
	"fmt"
	"time"
)

// servicePrincipalExpiringWindow 是"临期"计数的窗口：仍可用但在窗口内到期。
const servicePrincipalExpiringWindow = 7 * 24 * time.Hour

// ServicePrincipalStatistics 是服务令牌计数快照。
type ServicePrincipalStatistics struct {
	Active   int
	Expiring int
	Revoked  int
}

// Statistics 统计活跃、临期与已撤销的服务令牌。活跃指未撤销且未到期，包含轮换重叠期内
// 仍可认证的前序令牌；到期或重叠窗口结束后的令牌由 Expire 统一转为已撤销。
func (service *ServicePrincipalStore) Statistics(ctx context.Context, now time.Time) (ServicePrincipalStatistics, error) {
	stamp := now.UTC()
	var statistics ServicePrincipalStatistics
	err := service.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at > ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at > ? AND expires_at <= ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN revoked_at IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM service_principals`, stamp, stamp, stamp.Add(servicePrincipalExpiringWindow),
	).Scan(&statistics.Active, &statistics.Expiring, &statistics.Revoked)
	if err != nil {
		return ServicePrincipalStatistics{}, fmt.Errorf("service token statistics: %w", err)
	}
	return statistics, nil
}
