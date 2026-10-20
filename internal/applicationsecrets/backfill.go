package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

func (service *Service) resume(ctx context.Context) error {
	for service.state.Phase == "backfill" || service.state.Phase == "rotating" {
		if err := service.migrateBatch(ctx); err != nil {
			return err
		}
		if err := service.checkpoint("batch_committed"); err != nil {
			return err
		}
	}
	if service.state.Phase == "sanitize" {
		if err := service.sanitize(ctx); err != nil {
			return err
		}
	}
	if service.state.Phase != "ready" {
		return ErrUnhealthy
	}
	return nil
}

func (service *Service) migrateBatch(ctx context.Context) error {
	service.gate.Lock()
	defer service.gate.Unlock()
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "PRAGMA secure_delete = ON"); err != nil {
		return err
	}
	state, err := readState(ctx, tx)
	if err != nil {
		return err
	}
	legacy, err := hasLegacySSH(ctx, tx)
	if err != nil {
		return err
	}
	sources, err := sourcesForDatabase(ctx, tx, legacy)
	if err != nil {
		return err
	}
	if state.CursorSource == len(sources) {
		return service.finishMigration(ctx, tx, state)
	}
	if state.CursorSource < 0 || state.CursorSource >= len(sources) {
		return ErrUnhealthy
	}
	source := sources[state.CursorSource]
	resources, err := source.readBatch(ctx, tx, state.CursorID)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if err := service.rewriteResource(ctx, tx, rewriteInput{source: source, resource: resource, legacy: state.Phase == "backfill"}); err != nil {
			return err
		}
		state.CursorID = resource.id
	}
	if len(resources) == 0 {
		state.CursorSource++
		state.CursorID = ""
	}
	return service.commitCursor(ctx, tx, state)
}

func (service *Service) commitCursor(ctx context.Context, tx *sql.Tx, state lifecycleState) error {
	result, err := tx.ExecContext(ctx, `UPDATE application_secret_state SET cursor_source = ?, cursor_id = ?, updated_at = ? WHERE id = 1`,
		state.CursorSource, state.CursorID, service.now().Unix())
	if err := requireOne(result, err); err != nil {
		return err
	}
	if err := service.checkpoint("batch_before_commit"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	service.state = state
	return nil
}

type rewriteInput struct {
	source   resourceSource
	resource secretResource
	legacy   bool
}

func (service *Service) rewriteResource(ctx context.Context, tx *sql.Tx, input rewriteInput) error {
	resource := input.resource
	if len(resource.ciphertext) == 0 && len(resource.legacyPlaintext) == 0 {
		if resource.required {
			return ErrCiphertext
		}
		return nil
	}
	plaintext, err := service.resourcePlaintext(input)
	if err != nil {
		return ErrCiphertext
	}
	defer clear(plaintext)
	ciphertext, err := service.ring.Seal(plaintext, resource.aad)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, input.source.update, ciphertext, resource.id, resource.ciphertext)
	return requireOne(result, err)
}

func (service *Service) resourcePlaintext(input rewriteInput) ([]byte, error) {
	resource := input.resource
	if secretbox.IsV2Envelope(resource.ciphertext) {
		if len(resource.legacyPlaintext) != 0 {
			return nil, ErrCiphertext
		}
		return service.ring.Open(resource.ciphertext, resource.aad)
	}
	if !input.legacy {
		return nil, ErrCiphertext
	}
	switch input.source.name {
	case "totp":
		return service.root.Decrypt(resource.ciphertext)
	case "oidc_provider", "oidc_transaction":
		return service.root.DecryptEnvelope(resource.ciphertext, resource.aad)
	case "ssh":
		if len(resource.ciphertext) != 0 || len(resource.legacyPlaintext) == 0 {
			return nil, ErrCiphertext
		}
		return resource.legacyPlaintext, nil
	default:
		return nil, errors.New("unregistered application secret resource")
	}
}

func (service *Service) finishMigration(ctx context.Context, tx *sql.Tx, state lifecycleState) error {
	legacy := state.Phase == "backfill"
	if _, err := validateResources(ctx, tx, validationConfig{ring: service.ring, legacySSH: legacy, requireActive: true}); err != nil {
		return err
	}
	phase := "ready"
	if legacy {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE ssh_keys DROP COLUMN private_key"); err != nil {
			return err
		}
		phase = "sanitize"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE application_data_keys SET retire_after = ? WHERE state = 'decrypt_only' AND retire_after IS NULL`,
		service.now().Add(keyRetention).Unix()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE application_secret_state SET phase = ?, cursor_source = 0, cursor_id = '', updated_at = ? WHERE id = 1`, phase, service.now().Unix())
	if err := requireOne(result, err); err != nil {
		return err
	}
	if err := service.checkpoint("migration_before_complete"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	state.Phase, state.CursorSource, state.CursorID = phase, 0, ""
	service.state = state
	return nil
}

func (service *Service) sanitize(ctx context.Context) error {
	if _, err := service.db.ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	if err := checkpointSQLite(ctx, service.db); err != nil {
		return err
	}
	if err := service.checkpoint("sanitized"); err != nil {
		return err
	}
	result, err := service.db.ExecContext(ctx, `UPDATE application_secret_state SET phase = 'ready', updated_at = ? WHERE id = 1 AND phase = 'sanitize'`, service.now().Unix())
	if err := requireOne(result, err); err != nil {
		return err
	}
	service.state.Phase = "ready"
	return nil
}

func checkpointSQLite(ctx context.Context, db *sql.DB) error {
	var busy, pages, checkpointed int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return ErrLifecycleBusy
	}
	return nil
}
