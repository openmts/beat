package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

// EnrollmentStatistics 是入网聚合快照：只含计数与最老未决请求时间，不含任何标识或秘密。
type EnrollmentStatistics struct {
	Pending, Approved, Rejected, Expired, Claimed int
	OldestOpenRequestAt                           sql.NullTime
	OldestOpenRequestAge                          time.Duration
	ActiveGrants, ExpiredGrants                   int
	DisabledGrants, RevokedGrants                 int
}

// EnrollmentStatistics 汇总请求状态、最老未决请求与授权状态；供 Prometheus 抓取使用。
func (s *AgentEnrollmentStore) EnrollmentStatistics(ctx context.Context, now time.Time) (EnrollmentStatistics, error) {
	statistics := EnrollmentStatistics{}
	if err := s.countEnrollmentRequestsByStatus(ctx, &statistics); err != nil {
		return EnrollmentStatistics{}, err
	}
	if err := s.loadOldestOpenEnrollmentRequest(ctx, &statistics); err != nil {
		return EnrollmentStatistics{}, err
	}
	if err := s.countEnrollmentGrantsByState(ctx, now.UTC(), &statistics); err != nil {
		return EnrollmentStatistics{}, err
	}
	statistics.OldestOpenRequestAge = oldestOpenRequestAge(statistics.OldestOpenRequestAt, now.UTC())
	return statistics, nil
}

// countEnrollmentRequestsByStatus 只按白名单状态聚合；未知状态既不计数也不报错。
func (s *AgentEnrollmentStore) countEnrollmentRequestsByStatus(ctx context.Context, statistics *EnrollmentStatistics) error {
	rows, err := s.db.QueryContext(ctx, "SELECT status, COUNT(*) FROM agent_enrollment_requests GROUP BY status")
	if err != nil {
		return fmt.Errorf("count enrollment requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return fmt.Errorf("count enrollment requests: %w", err)
		}
		statistics.assignRequestStatus(agentenrollment.RequestStatus(status), count)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("count enrollment requests: %w", err)
	}
	return nil
}

func (statistics *EnrollmentStatistics) assignRequestStatus(status agentenrollment.RequestStatus, count int) {
	switch status {
	case agentenrollment.RequestPending:
		statistics.Pending = count
	case agentenrollment.RequestApproved:
		statistics.Approved = count
	case agentenrollment.RequestRejected:
		statistics.Rejected = count
	case agentenrollment.RequestExpired:
		statistics.Expired = count
	case agentenrollment.RequestClaimed:
		statistics.Claimed = count
	}
}

// loadOldestOpenEnrollmentRequest 取最老未认领请求；空队列是正常状态而不是错误。
func (s *AgentEnrollmentStore) loadOldestOpenEnrollmentRequest(ctx context.Context, statistics *EnrollmentStatistics) error {
	err := s.db.QueryRowContext(ctx, `SELECT first_request_at FROM agent_enrollment_requests
		WHERE status IN ('pending', 'approved') ORDER BY first_request_at ASC LIMIT 1`).Scan(
		&statistics.OldestOpenRequestAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load oldest open enrollment request: %w", err)
	}
	return nil
}

// countEnrollmentGrantsByState 按撤销、过期、停用与可用四态互斥聚合。
func (s *AgentEnrollmentStore) countEnrollmentGrantsByState(ctx context.Context, now time.Time, statistics *EnrollmentStatistics) error {
	err := s.db.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(CASE WHEN revoked_at IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at <= ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at > ? AND enabled = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at > ? AND enabled = 1 THEN 1 ELSE 0 END), 0)
		FROM agent_enrollment_grants`, now, now, now).Scan(&statistics.RevokedGrants, &statistics.ExpiredGrants,
		&statistics.DisabledGrants, &statistics.ActiveGrants)
	if err != nil {
		return fmt.Errorf("count enrollment grants: %w", err)
	}
	return nil
}

// oldestOpenRequestAge 把最老未决时间换算成非负年龄，时钟回拨时按零处理。
func oldestOpenRequestAge(oldest sql.NullTime, now time.Time) time.Duration {
	if !oldest.Valid {
		return 0
	}
	age := now.Sub(oldest.Time.UTC())
	if age < 0 {
		return 0
	}
	return age
}
