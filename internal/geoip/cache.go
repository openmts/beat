package geoip

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const maximumCacheEntries = 100000

func (s *Service) cached(ctx context.Context, item workItem) (resolution, bool) {
	var value resolution
	err := s.db.QueryRowContext(ctx, `SELECT scope,country_code,resolved_at FROM geoip_cache WHERE provider_revision=? AND source_fingerprint=? AND expires_at>?`, providerRevision(item.settings), item.fingerprint, s.now()).Scan(&value.scope, &value.country, &value.resolved)
	if err != nil {
		return resolution{}, false
	}
	_, err = s.db.ExecContext(ctx, `UPDATE geoip_cache SET accessed_at=? WHERE provider_revision=? AND source_fingerprint=?`, s.now(), providerRevision(item.settings), item.fingerprint)
	return value, err == nil
}

func (s *Service) complete(ctx context.Context, item workItem, result resolution) error {
	return s.write(ctx, func(tx transaction) error {
		current, err := scanSettings(tx.db.QueryRowContext(ctx, settingsSelect))
		if err != nil {
			return ErrUnavailable
		}
		if !sameRevision(current, providerRevision(item.settings)) {
			return nil
		}
		var fingerprint, lease string
		err = tx.db.QueryRowContext(ctx, `SELECT source_fingerprint,lease_hash FROM node_locations WHERE id=?`, item.id).Scan(&fingerprint, &lease)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		if fingerprint != item.fingerprint || lease != hashToken(item.token) {
			return nil
		}
		if result.errorCode != "" {
			_, err = tx.db.ExecContext(ctx, `UPDATE node_locations SET error_code=?,failures=failures+1,next_refresh_at=?,lease_hash='',lease_expires_at=NULL WHERE id=?`, result.errorCode, tx.now.Add(retryDelay(item.failures)), item.id)
			return err
		}
		if err = tx.cacheResult(item, result); err != nil {
			return err
		}
		_, err = tx.db.ExecContext(ctx, `UPDATE node_locations SET scope=?,country_code=?,provider=?,revision=?,resolved_at=?,error_code='',failures=0,next_refresh_at=?,lease_hash='',lease_expires_at=NULL WHERE id=?`, result.scope, result.country, item.settings.Provider, providerRevision(item.settings), result.resolved, tx.now.Add(refreshDelay(item.id)), item.id)
		return err
	})
}

func (tx transaction) cacheResult(item workItem, result resolution) error {
	expiry := tx.now.Add(30 * 24 * time.Hour)
	negative := result.country == ""
	if negative {
		expiry = tx.now.Add(time.Hour)
	}
	_, err := tx.db.ExecContext(tx.ctx, `INSERT INTO geoip_cache(provider_revision,source_fingerprint,scope,country_code,negative,resolved_at,expires_at,accessed_at)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(provider_revision,source_fingerprint) DO UPDATE SET scope=excluded.scope,country_code=excluded.country_code,negative=excluded.negative,
 resolved_at=excluded.resolved_at,expires_at=excluded.expires_at,accessed_at=excluded.accessed_at`, providerRevision(item.settings), item.fingerprint, result.scope, result.country, negative, result.resolved, expiry, tx.now)
	if err != nil {
		return ErrUnavailable
	}
	_, err = tx.db.ExecContext(tx.ctx, `DELETE FROM geoip_cache WHERE (provider_revision,source_fingerprint) IN
 (SELECT provider_revision,source_fingerprint FROM geoip_cache ORDER BY accessed_at DESC,provider_revision,source_fingerprint LIMIT -1 OFFSET ?)`, maximumCacheEntries)
	return err
}

func (s *Service) Ready(ctx context.Context) error {
	current, err := scanSettings(s.db.QueryRowContext(ctx, settingsSelect))
	if err != nil {
		return ErrUnavailable
	}
	var readable int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_locations WHERE next_refresh_at IS NOT NULL AND lease_expires_at IS NULL`).Scan(&readable); err != nil {
		return ErrUnavailable
	}
	if current.Provider != "mmdb" {
		return nil
	}
	s.mmdb.mu.RLock()
	defer s.mmdb.mu.RUnlock()
	if s.mmdb.reader == nil {
		return ErrUnavailable
	}
	return nil
}
