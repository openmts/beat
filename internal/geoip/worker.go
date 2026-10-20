package geoip

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

const leaseDuration = 2 * time.Minute

const workerInterval = time.Second

type workItem struct {
	id, mode, fingerprint, source, token string
	settings                             model.GeoIPSettings
	credential                           string
	failures                             int
}

type resolution struct {
	scope, country, errorCode string
	resolved                  time.Time
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(workerInterval)
	defer ticker.Stop()
	maintenance := time.NewTicker(time.Minute)
	defer maintenance.Stop()
	for {
		worked, err := s.processNext(ctx)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if worked && err == nil {
			s.maintainDue(ctx, maintenance.C)
			continue
		}
		if err = s.waitWork(ctx, ticker.C, maintenance.C); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (s *Service) maintainDue(ctx context.Context, ticks <-chan time.Time) {
	select {
	case <-ticks:
		_ = s.Prune(ctx)
	default:
	}
}

func (s *Service) waitWork(ctx context.Context, ticks, maintenance <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticks:
		return nil
	case <-maintenance:
		return s.Prune(ctx)
	}
}

// Process 每次领取一个有界任务；网络与DNS始终在SQLite事务外。
func (s *Service) Process(ctx context.Context) error {
	_, err := s.processNext(ctx)
	return err
}

func (s *Service) processNext(ctx context.Context) (worked bool, resultErr error) {
	defer func() { s.observe(resultErr) }()
	s.policyMu.RLock()
	defer s.policyMu.RUnlock()
	item, err := s.claim(ctx)
	if err != nil || item == nil {
		return false, err
	}
	lookupCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	result := s.resolve(lookupCtx, *item)
	return true, s.complete(ctx, *item, result)
}

func (s *Service) claim(ctx context.Context) (*workItem, error) {
	var item *workItem
	err := s.write(ctx, func(tx transaction) error {
		settings, err := scanSettings(tx.db.QueryRowContext(ctx, settingsSelect))
		if err != nil {
			return ErrUnavailable
		}
		if settings.Provider == "disabled" || (remoteProvider(settings.Provider) && !settings.RemoteConsent) {
			return nil
		}
		value := workItem{settings: settings}
		var encrypted []byte
		err = tx.db.QueryRowContext(ctx, `SELECT id,mode,source_fingerprint,source_encrypted,failures FROM node_locations
 WHERE mode IN('observed','managed_target') AND source_encrypted IS NOT NULL AND next_refresh_at<=?
 AND (lease_expires_at IS NULL OR lease_expires_at<=?) ORDER BY next_refresh_at,id LIMIT 1`, tx.now, tx.now).Scan(&value.id, &value.mode, &value.fingerprint, &encrypted, &value.failures)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		if err = tx.prepareWork(&value, encrypted); err != nil {
			return err
		}
		value.token = uuid.NewString()
		if _, err = tx.db.ExecContext(ctx, `UPDATE node_locations SET lease_hash=?,lease_expires_at=?,worker_id='geoip',last_attempt_at=? WHERE id=?`, hashToken(value.token), tx.now.Add(leaseDuration), tx.now, value.id); err != nil {
			return ErrUnavailable
		}
		item = &value
		return nil
	})
	return item, err
}

func (tx transaction) prepareWork(item *workItem, encrypted []byte) error {
	raw, err := tx.access.Open(encrypted, secretbox.GeoIPSourceAAD(item.id, item.mode))
	if err != nil {
		return ErrUnavailable
	}
	defer clear(raw)
	item.source = string(raw)
	if remoteProvider(item.settings.Provider) && item.settings.HasCredential {
		if err = tx.db.QueryRowContext(tx.ctx, `SELECT credential_encrypted FROM geoip_settings WHERE id='global'`).Scan(&encrypted); err != nil {
			return ErrUnavailable
		}
		raw, err = tx.access.Open(encrypted, secretbox.GeoIPCredentialAAD())
		if err != nil {
			return ErrUnavailable
		}
		defer clear(raw)
		item.credential = string(raw)
	}
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) resolve(ctx context.Context, item workItem) resolution {
	if cached, ok := s.cached(ctx, item); ok {
		return cached
	}
	sources := []string{item.source}
	if item.mode == "managed_target" {
		var err error
		sources, err = resolveTarget(ctx, item.source)
		if err != nil {
			return resolution{errorCode: "target_resolution_failed"}
		}
	}
	var result resolution
	for _, source := range sources {
		current := s.resolveAddress(ctx, item, source)
		if current.errorCode != "" {
			return current
		}
		if result.scope != "" && (current.scope != result.scope || current.country != result.country) {
			return resolution{errorCode: "ambiguous_target"}
		}
		result = current
	}
	return result
}

func (s *Service) resolveAddress(ctx context.Context, item workItem, source string) resolution {
	ip, scope := Classify(source)
	result := resolution{scope: scope, resolved: s.now()}
	if scope != "public" {
		return result
	}
	var err error
	if item.settings.Provider == "mmdb" {
		result.country, err = s.mmdb.Lookup(ip.String())
	} else {
		result.country, err = s.remoteLookup(ctx, providerLookup{provider: item.settings.Provider, source: ip.String(), credential: item.credential})
	}
	if err != nil {
		result.errorCode = "provider_unavailable"
	}
	return result
}

func retryDelay(failures int) time.Duration {
	delays := []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour, 24 * time.Hour}
	if failures >= len(delays) {
		return delays[len(delays)-1]
	}
	if failures < 0 {
		failures = 0
	}
	return delays[failures]
}

func refreshDelay(id string) time.Duration {
	digest := sha256.Sum256([]byte(id))
	return 30*24*time.Hour + time.Duration(int(digest[0])*60)*time.Second
}

func sameRevision(settings model.GeoIPSettings, revision string) bool {
	return strings.EqualFold(providerRevision(settings), revision)
}
