package geoip

import (
	"context"
	"database/sql"
	"time"

	"github.com/beat/backend/internal/secretbox"
)

type ObservedSource struct {
	NodeID string
	Source string
	Access secretbox.ResourceAccess
	Now    time.Time
}

// RecordObserved 只在已复验节点凭证的同一事务内执行；不解析DNS或调用provider。
func RecordObserved(ctx context.Context, tx *sql.Tx, input ObservedSource) error {
	ip, scope := Classify(input.Source)
	if !ip.IsValid() {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO node_locations(id,updated_at) VALUES(?,?) ON CONFLICT(id) DO NOTHING`, input.NodeID, input.Now); err != nil {
		return err
	}
	var mode, previous string
	if err := tx.QueryRowContext(ctx, `SELECT mode,source_fingerprint FROM node_locations WHERE id=?`, input.NodeID).Scan(&mode, &previous); err != nil {
		return err
	}
	if mode != "observed" {
		return nil
	}
	keyed, ok := input.Access.(secretbox.GeoIPFingerprinter)
	if !ok {
		return ErrUnavailable
	}
	raw := []byte(ip.String())
	defer clear(raw)
	fingerprint, err := keyed.GeoIPFingerprint(raw)
	if err != nil {
		return ErrUnavailable
	}
	if fingerprint == previous {
		return nil
	}
	sealed, err := input.Access.Seal(raw, secretbox.GeoIPSourceAAD(input.NodeID, mode))
	if err != nil {
		return ErrUnavailable
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_locations SET source_encrypted=?,source_fingerprint=?,scope=?,
 country_code='',resolved_at=NULL,provider='',revision='',error_code='',failures=0,lease_hash='',lease_expires_at=NULL,
 next_refresh_at=CASE WHEN (SELECT provider FROM geoip_settings WHERE id='global')!='disabled' THEN ? ELSE NULL END,
 updated_at=? WHERE id=? AND mode='observed'`, sealed, fingerprint, scope, input.Now, input.Now, input.NodeID)
	return err
}
