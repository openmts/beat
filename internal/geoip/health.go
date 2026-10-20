package geoip

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Snapshot 是不含来源、节点ID、国家或凭据的低基数健康快照。
type Snapshot struct {
	Provider     string     `json:"provider"`
	Due          int64      `json:"due"`
	Leased       int64      `json:"leased"`
	Stale        int64      `json:"stale"`
	CacheEntries int64      `json:"cache_entries"`
	OldestDueAt  *time.Time `json:"oldest_due_at,omitempty"`
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	var value Snapshot
	var oldest sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT provider FROM geoip_settings WHERE id='global'`).Scan(&value.Provider)
	if err != nil {
		return value, ErrUnavailable
	}
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(next_refresh_at<=?),0),COALESCE(SUM(lease_expires_at>?),0),
 COALESCE(SUM(resolved_at<?),0) FROM node_locations`, s.now(), s.now(), s.now().Add(-90*24*time.Hour)).Scan(&value.Due, &value.Leased, &value.Stale)
	if err != nil {
		return value, ErrUnavailable
	}
	err = s.db.QueryRowContext(ctx, `SELECT next_refresh_at FROM node_locations WHERE next_refresh_at<=? ORDER BY next_refresh_at LIMIT 1`, s.now()).Scan(&oldest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return value, ErrUnavailable
	}
	value.OldestDueAt = nullableTime(oldest)
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM geoip_cache`).Scan(&value.CacheEntries); err != nil {
		return value, ErrUnavailable
	}
	return value, nil
}

// Prune 一次最多删除1000个过期缓存，不触碰现有国家和来源。
func (s *Service) Prune(ctx context.Context) (resultErr error) {
	defer func() { s.observe(resultErr) }()
	_, err := s.db.ExecContext(ctx, `DELETE FROM geoip_cache WHERE (provider_revision,source_fingerprint) IN
 (SELECT provider_revision,source_fingerprint FROM geoip_cache WHERE expires_at<=? ORDER BY expires_at LIMIT 1000)`, s.now())
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
