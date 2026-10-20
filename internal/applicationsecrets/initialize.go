package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/secretbox"
)

func (service *Service) initialize(ctx context.Context) error {
	if _, err := recoverRootJournal(ctx, service.db, service.directory); err != nil {
		return err
	}
	state, err := readState(ctx, service.db)
	if err != nil {
		return err
	}
	service.state = state
	if err := service.loadRoot(ctx); err != nil {
		return err
	}
	if state.Phase == "pending" {
		if err := service.bootstrap(ctx); err != nil {
			return err
		}
	}
	if err := service.reload(ctx); err != nil {
		return err
	}
	if err := service.resume(ctx); err != nil {
		return err
	}
	if err := service.initializeNotifications(ctx); err != nil {
		return err
	}
	_, err = validateResources(ctx, service.db, validationConfig{ring: service.ring, requireActive: true})
	return err
}

func (service *Service) loadRoot(ctx context.Context) error {
	path := filepath.Join(service.directory, "admin-data.key")
	key, err := readRootKey(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := service.allowNewRoot(ctx); err != nil {
			return err
		}
		key = make([]byte, rootKeyBytes)
		if _, err := io.ReadFull(service.random, key); err != nil {
			clear(key)
			return err
		}
		if err := publishNewPrivateFile(path, key); err != nil {
			clear(key)
			return err
		}
	} else if err != nil {
		return err
	}
	defer clear(key)
	service.root, err = secretbox.NewFromKey(key, service.random)
	return err
}

func (service *Service) allowNewRoot(ctx context.Context) error {
	if service.state.Phase != "pending" || service.state.Generation != 0 || service.state.RootFingerprint != "" {
		return ErrUnhealthy
	}
	if err := rejectPendingRestore(service.directory); err != nil {
		return err
	}
	var count int
	err := service.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM application_data_keys) +
		(SELECT COUNT(*) FROM admin_users WHERE length(totp_secret_encrypted) > 0 OR totp_enabled_at IS NOT NULL) +
		(SELECT COUNT(*) FROM admin_oidc_providers WHERE length(client_secret_encrypted) > 0) +
		(SELECT COUNT(*) FROM admin_oidc_transactions WHERE length(code_verifier_encrypted) > 0) +
		(SELECT COUNT(*) FROM ssh_keys WHERE length(private_key_encrypted) > 0)`).Scan(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return ErrUnhealthy
	}
	for _, name := range []string{".root-key-rotation", ".root-key-rotation.prepare"} {
		if _, err := os.Lstat(filepath.Join(service.directory, name)); !errors.Is(err, os.ErrNotExist) {
			return ErrUnhealthy
		}
	}
	return nil
}

func (service *Service) bootstrap(ctx context.Context) (resultErr error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := readState(ctx, tx)
	if err != nil {
		return err
	}
	if state.Phase != "pending" || state.Generation != 0 {
		return ErrUnhealthy
	}
	id, err := service.insertKey(ctx, tx, 1)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE application_secret_state SET phase = 'backfill', generation = 1,
		root_generation = 1, root_fingerprint = ?, active_key_id = ?, updated_at = ? WHERE id = 1 AND phase = 'pending'`,
		service.root.RootFingerprint(), id, service.now().Unix())
	if err := requireOne(result, err); err != nil {
		return err
	}
	if err := service.checkpoint("bootstrap_before_commit"); err != nil {
		return err
	}
	return tx.Commit()
}

func (service *Service) insertKey(ctx context.Context, tx *sql.Tx, generation int64) (string, error) {
	key, err := secretbox.GenerateDataKey(service.random)
	if err != nil {
		return "", err
	}
	defer clear(key.Material)
	wrapped, err := service.root.WrapDataKey(key, generation)
	if err != nil {
		return "", err
	}
	now := service.now().Unix()
	result, err := tx.ExecContext(ctx, `INSERT INTO application_data_keys
		(id, wrapped_key, generation, state, created_at, activated_at) VALUES (?, ?, ?, 'active', ?, ?)`, key.ID, wrapped, generation, now, now)
	if err := requireOne(result, err); err != nil {
		return "", err
	}
	return key.ID, nil
}

func requireOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("application secret update did not affect exactly one row")
	}
	return nil
}

func (service *Service) reload(ctx context.Context) error {
	state, err := readState(ctx, service.db)
	if err != nil {
		return err
	}
	ring, err := loadKeyring(ctx, service.db, keyringConfig{root: service.root, state: state, random: service.random})
	if err != nil {
		return err
	}
	service.state, service.ring = state, ring
	return nil
}
