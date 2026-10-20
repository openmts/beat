package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

type databaseReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type lifecycleState struct {
	Phase                      string
	Generation, RootGeneration int64
	RootFingerprint, ActiveID  string
	CursorSource               int
	CursorID                   string
	RotationID                 string
	UpdatedAt                  int64
}

type registryKey struct {
	secretbox.WrappedDataKey
	State                  string
	CreatedAt, ActivatedAt int64
	RetireAfter            sql.NullInt64
}

type keyring struct {
	active  string
	ciphers map[string]*secretbox.DataCipher
}

func readState(ctx context.Context, database databaseReader) (lifecycleState, error) {
	var state lifecycleState
	err := database.QueryRowContext(ctx, `SELECT phase, generation, root_generation, root_fingerprint,
		active_key_id, cursor_source, cursor_id, updated_at, rotation_id FROM application_secret_state WHERE id = 1`).Scan(
		&state.Phase, &state.Generation, &state.RootGeneration, &state.RootFingerprint, &state.ActiveID,
		&state.CursorSource, &state.CursorID, &state.UpdatedAt, &state.RotationID)
	if err == nil && state.Generation > 1 {
		id, parseErr := uuid.Parse(state.RotationID)
		if parseErr != nil || id.String() != state.RotationID {
			return state, ErrUnhealthy
		}
	}
	return state, err
}

func readRegistry(ctx context.Context, database databaseReader) (keys []registryKey, resultErr error) {
	rows, err := database.QueryContext(ctx, `SELECT id, wrapped_key, generation, state, created_at, activated_at,
		retire_after FROM application_data_keys ORDER BY generation, id LIMIT ?`, maximumDataKeys+1)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var key registryKey
		if err := rows.Scan(&key.ID, &key.Wrapped, &key.Generation, &key.State, &key.CreatedAt, &key.ActivatedAt, &key.RetireAfter); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func loadKeyring(ctx context.Context, database databaseReader, config keyringConfig) (*keyring, error) {
	keys, err := readRegistry(ctx, database)
	if err != nil {
		return nil, err
	}
	if len(keys) < 1 || len(keys) > maximumDataKeys || config.state.RootFingerprint != config.root.RootFingerprint() || config.state.RootGeneration < 1 {
		return nil, ErrUnhealthy
	}
	ring := &keyring{active: config.state.ActiveID, ciphers: make(map[string]*secretbox.DataCipher)}
	activeCount := 0
	for _, key := range keys {
		if err := validateRegistryKey(key, config.state); err != nil {
			return nil, err
		}
		if key.State == "active" {
			activeCount++
		}
		cipher, err := unwrapCipher(config, key)
		if err != nil {
			return nil, err
		}
		ring.ciphers[key.ID] = cipher
	}
	if activeCount != 1 || ring.ciphers[ring.active] == nil {
		return nil, ErrUnhealthy
	}
	return ring, nil
}

type keyringConfig struct {
	root   *secretbox.Manager
	state  lifecycleState
	random io.Reader
}

func validateRegistryKey(key registryKey, state lifecycleState) error {
	if key.Generation < 1 || key.Generation > state.Generation || key.CreatedAt <= 0 || key.ActivatedAt < key.CreatedAt {
		return ErrUnhealthy
	}
	if key.State == "active" {
		if key.ID != state.ActiveID || key.Generation != state.Generation || key.RetireAfter.Valid {
			return ErrUnhealthy
		}
	} else if key.State != "decrypt_only" || key.ID == state.ActiveID {
		return ErrUnhealthy
	} else if state.Phase == "ready" && (!key.RetireAfter.Valid || key.RetireAfter.Int64 < key.ActivatedAt) {
		return ErrUnhealthy
	}
	return nil
}

func unwrapCipher(config keyringConfig, wrapped registryKey) (*secretbox.DataCipher, error) {
	key, err := config.root.UnwrapDataKey(wrapped.WrappedDataKey)
	if err != nil {
		return nil, ErrUnhealthy
	}
	defer clear(key.Material)
	return secretbox.NewDataCipher(key.ID, key.Material, config.random)
}

func (ring *keyring) Seal(plaintext []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	return ring.ciphers[ring.active].Seal(plaintext, aad)
}

func (ring *keyring) Open(ciphertext []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	id, err := secretbox.EnvelopeDataKeyID(ciphertext)
	if err != nil {
		return nil, ErrCiphertext
	}
	cipher := ring.ciphers[id]
	if cipher == nil {
		return nil, ErrCiphertext
	}
	return cipher.Open(ciphertext, aad)
}
