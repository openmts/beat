package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/beat/backend/internal/secretbox"
)

// validateOIDCCiphertexts 认证归档中的 OIDC 与 TOTP 密文，缺密钥不能跳过检查。
func validateOIDCCiphertexts(ctx context.Context, sqlitePath, dataKeyPath string) (resultErr error) {
	if handled, err := validateV9Secrets(ctx, sqlitePath, dataKeyPath); handled || err != nil {
		return err
	}
	secrets, err := archivedSecretManager(dataKeyPath)
	if err != nil {
		return err
	}
	database, err := openArchivedSQLite(sqlitePath)
	if err != nil {
		return fmt.Errorf("open archived secrets database: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	validation := archivedSecretValidation{database: database, secrets: secrets}
	for _, source := range archivedSecretSources() {
		if err := validation.check(ctx, source); err != nil {
			return err
		}
	}
	return nil
}

func archivedSecretManager(path string) (*secretbox.Manager, error) {
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read archived administrator data key: %w", err)
	}
	return secretbox.NewFromKey(key, nil)
}

type archivedSecretValidation struct {
	database *sql.DB
	secrets  *secretbox.Manager
}

func (validation archivedSecretValidation) check(ctx context.Context, source archivedSecretSource) (resultErr error) {
	rows, err := validation.database.QueryContext(ctx, source.query)
	if isNoSuchTable(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query archived %s: %w", source.name, err)
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		secret, err := source.scan(rows)
		if err != nil {
			return fmt.Errorf("read archived %s: %w", source.name, err)
		}
		if err := validation.authenticate(secret); err != nil {
			return fmt.Errorf("validate archived %s: %w", source.name, err)
		}
	}
	return rows.Err()
}

func (validation archivedSecretValidation) authenticate(secret archivedSecret) error {
	if len(secret.ciphertext) == 0 {
		if secret.required {
			return errors.New("enabled secret is missing its ciphertext")
		}
		return nil
	}
	if validation.secrets == nil {
		return errors.New("archived encrypted credentials require the matching administrator data key")
	}
	var err error
	if secret.legacy {
		_, err = validation.secrets.Decrypt(secret.ciphertext)
	} else {
		_, err = validation.secrets.DecryptEnvelope(secret.ciphertext, secret.aad)
	}
	if err != nil {
		return errors.New("archived encrypted credential failed authentication")
	}
	return nil
}

func isNoSuchTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}
