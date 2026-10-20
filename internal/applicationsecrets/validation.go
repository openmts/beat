package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"
	"net/url"

	_ "modernc.org/sqlite"

	"github.com/beat/backend/internal/secretbox"
)

type validationConfig struct {
	ring                     *keyring
	legacySSH, requireActive bool
}

func validateResources(ctx context.Context, database databaseReader, config validationConfig) (map[string]int, error) {
	legacy, err := hasLegacySSH(ctx, database)
	if err != nil {
		return nil, err
	}
	if legacy && !config.legacySSH {
		return nil, ErrUnhealthy
	}
	references := make(map[string]int)
	sources, err := sourcesForDatabase(ctx, database, legacy)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if err := validateSource(ctx, database, sourceValidation{source: source, config: config, references: references}); err != nil {
			return nil, err
		}
	}
	frames, err := operationFrameReferences(ctx, database, config)
	if err != nil {
		return nil, err
	}
	for id, count := range frames {
		references[id] += count
	}
	return references, nil
}

type sourceValidation struct {
	source     resourceSource
	config     validationConfig
	references map[string]int
}

func validateSource(ctx context.Context, database databaseReader, validation sourceValidation) error {
	cursor := ""
	for {
		resources, err := validation.source.readBatch(ctx, database, cursor)
		if err != nil {
			return err
		}
		if len(resources) == 0 {
			return nil
		}
		for _, resource := range resources {
			id, err := validateResource(resource, validation.config)
			if err != nil {
				return err
			}
			if id != "" {
				validation.references[id]++
			}
			cursor = resource.id
		}
	}
}

func validateResource(resource secretResource, config validationConfig) (string, error) {
	if len(resource.legacyPlaintext) != 0 {
		return "", ErrCiphertext
	}
	if len(resource.ciphertext) == 0 {
		if resource.required {
			return "", ErrCiphertext
		}
		return "", nil
	}
	id, err := secretbox.EnvelopeDataKeyID(resource.ciphertext)
	if err != nil {
		return "", ErrCiphertext
	}
	if config.requireActive && id != config.ring.active {
		return "", ErrUnhealthy
	}
	plaintext, err := config.ring.Open(resource.ciphertext, resource.aad)
	clear(plaintext)
	if err != nil {
		return "", ErrCiphertext
	}
	return id, nil
}

// ValidateSnapshot 只读认证 v9 快照中的注册表和全部资源；旧格式由备份包显式选择。
func ValidateSnapshot(ctx context.Context, sqlitePath, rootKeyPath string) (resultErr error) {
	database, err := openSnapshot(sqlitePath)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	state, err := readState(ctx, database)
	if err != nil {
		return err
	}
	if state.Phase != "ready" || state.CursorSource != 0 || state.CursorID != "" {
		return ErrUnhealthy
	}
	if err := validateNotificationState(ctx, database); err != nil {
		return err
	}
	root, err := snapshotRoot(rootKeyPath)
	if err != nil {
		return err
	}
	ring, err := loadKeyring(ctx, database, keyringConfig{root: root, state: state})
	if err != nil {
		return err
	}
	_, err = validateResources(ctx, database, validationConfig{ring: ring, requireActive: true})
	return err
}

func snapshotRoot(path string) (*secretbox.Manager, error) {
	key, err := readRootKey(path)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return secretbox.NewFromKey(key, nil)
}

func openSnapshot(path string) (*sql.DB, error) {
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	return sql.Open("sqlite", uri+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
}
