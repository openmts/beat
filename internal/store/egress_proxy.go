package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

var ErrEgressProxyConflict = errors.New("egress proxy settings changed")

type EgressProxyRecord struct {
	Enabled   bool
	Encrypted []byte
	Revision  int64
	UpdatedAt time.Time
}

const egressProxySelect = `SELECT enabled,proxy_secret_encrypted,revision,updated_at FROM egress_proxy_settings WHERE id=1`

func (store *EgressProxyStore) Get(ctx context.Context) (EgressProxyRecord, error) {
	return scanEgressProxy(store.db.QueryRowContext(ctx, egressProxySelect))
}

type EgressProxyStore struct {
	db *sql.DB
}

func NewEgressProxyStore(db *sql.DB) *EgressProxyStore {
	return &EgressProxyStore{db: db}
}

func (store *EgressProxyStore) UpdateEncrypted(
	ctx context.Context, enabled bool, encrypted []byte, expectedRevision int64,
) (EgressProxyRecord, error) {
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return EgressProxyRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return EgressProxyRecord{}, err
	}
	before, err := scanEgressProxy(tx.QueryRowContext(ctx, egressProxySelect))
	if err != nil {
		return EgressProxyRecord{}, err
	}
	updatedAt := model.NowUTC()
	result, err := tx.ExecContext(ctx, `UPDATE egress_proxy_settings
		SET enabled=?,proxy_secret_encrypted=?,revision=revision+1,updated_at=?
		WHERE id=1 AND revision=?`, enabled, encrypted, updatedAt, expectedRevision)
	if err != nil {
		return EgressProxyRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return EgressProxyRecord{}, err
	}
	if affected != 1 {
		return EgressProxyRecord{}, ErrEgressProxyConflict
	}
	after, err := scanEgressProxy(tx.QueryRowContext(ctx, egressProxySelect))
	if err != nil {
		return EgressProxyRecord{}, err
	}
	if err := mutation.finish(egressProxyEvidence(before, after)); err != nil {
		return EgressProxyRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return EgressProxyRecord{}, err
	}
	return after, nil
}

func scanEgressProxy(row rowScanner) (EgressProxyRecord, error) {
	var value EgressProxyRecord
	var encrypted []byte
	err := row.Scan(&value.Enabled, &encrypted, &value.Revision, &value.UpdatedAt)
	value.Encrypted = encrypted
	return value, err
}

func egressProxyEvidence(before, after EgressProxyRecord) metadataEvidence {
	fields := make([]string, 0, 3)
	if before.Enabled != after.Enabled {
		fields = append(fields, "enabled")
	}
	if !bytes.Equal(before.Encrypted, after.Encrypted) {
		fields = append(fields, "proxy_secret")
	}
	if before.Revision != after.Revision {
		fields = append(fields, "revision")
	}
	return metadataEvidence{Action: "egress_proxy.update", Type: "egress_proxy", ID: "global", Fields: fields}
}
