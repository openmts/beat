package applicationsecrets

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/beat/backend/internal/secretbox"
)

const (
	rotationJournalDir  = ".root-key-rotation"
	rotationPrepareDir  = ".root-key-rotation.prepare"
	rotationCompleteDir = ".root-key-rotation.complete"
)

var rotationDirectories = []string{rotationJournalDir, rotationPrepareDir, rotationCompleteDir}

type RootRotationConfig struct {
	DataDir, DatabasePath string
	Random                io.Reader
	fault                 func(string) error
}

type rootRotator struct {
	config    RootRotationConfig
	db        *sql.DB
	old, next *secretbox.Manager
	journal   rootJournal
}

func RotateRoot(ctx context.Context, config RootRotationConfig) error {
	if os.Geteuid() != 0 {
		return errors.New("root-key rotation requires root and a stopped server")
	}
	return rotateRoot(ctx, config)
}

func rotateRoot(ctx context.Context, config RootRotationConfig) (resultErr error) {
	lease, err := AcquireLease(config.DataDir)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	if err := rejectPendingRestore(config.DataDir); err != nil {
		return err
	}
	database, err := openRootRotationDB(config)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	recovered, err := recoverRootJournal(ctx, database, config.DataDir)
	if err != nil || recovered {
		return err
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	rotator := &rootRotator{config: config, db: database}
	if err := rotator.prepare(ctx); err != nil {
		return err
	}
	if err := rotator.rewrap(ctx); err != nil {
		return err
	}
	if err := checkpointSQLite(ctx, database); err != nil {
		return err
	}
	if err := rotator.checkpoint("root_db_committed"); err != nil {
		return err
	}
	if err := publishJournalRoot(config.DataDir, "next.key"); err != nil {
		return err
	}
	if err := rotator.checkpoint("root_key_published"); err != nil {
		return err
	}
	if err := ValidateSnapshot(ctx, config.DatabasePath, filepath.Join(config.DataDir, "admin-data.key")); err != nil {
		return err
	}
	if err := rotator.checkpoint("root_verified"); err != nil {
		return err
	}
	return finishRootJournal(config.DataDir, rotator.checkpoint)
}

func (rotator *rootRotator) prepare(ctx context.Context) error {
	key, err := readRootKey(filepath.Join(rotator.config.DataDir, "admin-data.key"))
	if err != nil {
		return err
	}
	defer clear(key)
	rotator.old, err = secretbox.NewFromKey(key, rotator.config.Random)
	if err != nil {
		return err
	}
	state, err := validateWithRoot(ctx, rotator.db, rotator.old)
	if err != nil {
		return err
	}
	if state.RootGeneration == math.MaxInt64 {
		return ErrUnhealthy
	}
	prepare := filepath.Join(rotator.config.DataDir, rotationPrepareDir)
	if err := createRotationDirectory(prepare); err != nil {
		return err
	}
	if err := rotator.checkpoint("root_prepare_created"); err != nil {
		return err
	}
	if err := publishNewPrivateFile(filepath.Join(prepare, "old.key"), key); err != nil {
		return err
	}
	if err := rotator.checkpoint("root_old_saved"); err != nil {
		return err
	}
	if err := rotator.prepareNextRoot(prepare); err != nil {
		return err
	}
	rotator.journal = rootJournal{Version: 1, PreviousFingerprint: rotator.old.RootFingerprint(), NextFingerprint: rotator.next.RootFingerprint(),
		PreviousGeneration: state.RootGeneration, NextGeneration: state.RootGeneration + 1}
	return rotator.publishJournal(prepare)
}

func (rotator *rootRotator) prepareNextRoot(prepare string) error {
	next := make([]byte, rootKeyBytes)
	defer clear(next)
	if _, err := io.ReadFull(rotator.config.Random, next); err != nil {
		return err
	}
	manager, err := secretbox.NewFromKey(next, rotator.config.Random)
	if err != nil {
		return err
	}
	rotator.next = manager
	if rotator.old.RootFingerprint() == rotator.next.RootFingerprint() {
		return errors.New("new root key must differ")
	}
	if err := publishNewPrivateFile(filepath.Join(prepare, "next.key"), next); err != nil {
		return err
	}
	return rotator.checkpoint("root_next_saved")
}

func (rotator *rootRotator) rewrap(ctx context.Context) error {
	tx, err := rotator.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	keys, err := readRegistry(ctx, tx)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := rotator.rewrapKey(ctx, tx, key); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE application_secret_state SET root_fingerprint = ?, root_generation = ?
		WHERE id = 1 AND root_fingerprint = ? AND root_generation = ? AND phase = 'ready'`,
		rotator.journal.NextFingerprint, rotator.journal.NextGeneration, rotator.journal.PreviousFingerprint, rotator.journal.PreviousGeneration)
	if err := requireOne(result, err); err != nil {
		return err
	}
	if err := rotator.checkpoint("root_before_commit"); err != nil {
		return err
	}
	return tx.Commit()
}

func (rotator *rootRotator) rewrapKey(ctx context.Context, tx *sql.Tx, key registryKey) error {
	material, err := rotator.old.UnwrapDataKey(key.WrappedDataKey)
	if err != nil {
		return ErrCiphertext
	}
	defer clear(material.Material)
	wrapped, err := rotator.next.WrapDataKey(material, key.Generation)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE application_data_keys SET wrapped_key = ? WHERE id = ? AND wrapped_key = ?", wrapped, key.ID, key.Wrapped)
	return requireOne(result, err)
}

func openRootRotationDB(config RootRotationConfig) (*sql.DB, error) {
	directory, err := canonicalDataPath(config.DataDir)
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(config.DatabasePath)
	if err != nil || filepath.Dir(path) != directory {
		return nil, errors.New("rotation database must be inside the data directory")
	}
	owner, err := privateDirectoryOwner(directory)
	if err != nil {
		return nil, err
	}
	file, err := openPrivateFile(path, unix.O_RDWR, owner)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	return sql.Open("sqlite", uri+"?mode=rw&_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)")
}

func (rotator *rootRotator) checkpoint(phase string) error {
	if rotator.config.fault != nil {
		return rotator.config.fault(phase)
	}
	return nil
}
