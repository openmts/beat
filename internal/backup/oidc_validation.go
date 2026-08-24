package backup

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/secretbox"
)

// validateOIDCCiphertexts 使用归档根密钥认证每个 OIDC 提供方密钥与 PKCE
// 验证器密文（信封格式 1 + 资源绑定 AAD），证明归档可在恢复后解密。
func validateOIDCCiphertexts(ctx context.Context, sqlitePath, dataKeyPath string) error {
	key, err := os.ReadFile(dataKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 旧格式归档不含根密钥；此时数据库不应含 OIDC 密文（由下面查询覆盖）。
			return nil
		}
		return fmt.Errorf("read archived administrator data key: %w", err)
	}
	secrets, err := secretbox.NewFromKey(key, nil)
	if err != nil {
		return fmt.Errorf("construct archived secret manager: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(sqlitePath)+"?mode=ro&_query_only=1")
	if err != nil {
		return fmt.Errorf("open archived sqlite read-only: %w", err)
	}
	defer func() { _ = db.Close() }()

	providerRows, err := db.QueryContext(ctx,
		"SELECT id, configuration_version, client_secret_encrypted FROM admin_oidc_providers")
	if err != nil {
		if isNoSuchTable(err) {
			return nil
		}
		return fmt.Errorf("query archived oidc providers: %w", err)
	}
	for providerRows.Next() {
		var id string
		var version int
		var ciphertext []byte
		if err := providerRows.Scan(&id, &version, &ciphertext); err != nil {
			_ = providerRows.Close()
			return fmt.Errorf("scan archived oidc provider: %w", err)
		}
		if len(ciphertext) == 0 {
			continue
		}
		if _, err := secrets.DecryptEnvelope(ciphertext, oidc.ProviderSecretAAD(id, version)); err != nil {
			_ = providerRows.Close()
			return fmt.Errorf("archived oidc provider secret failed authentication: %w", err)
		}
	}
	if err := providerRows.Close(); err != nil {
		return fmt.Errorf("close archived oidc provider rows: %w", err)
	}

	transactionRows, err := db.QueryContext(ctx,
		"SELECT id, provider_id, code_verifier_encrypted FROM admin_oidc_transactions")
	if err != nil {
		if isNoSuchTable(err) {
			return nil
		}
		return fmt.Errorf("query archived oidc transactions: %w", err)
	}
	for transactionRows.Next() {
		var id, providerID string
		var ciphertext []byte
		if err := transactionRows.Scan(&id, &providerID, &ciphertext); err != nil {
			_ = transactionRows.Close()
			return fmt.Errorf("scan archived oidc transaction: %w", err)
		}
		if len(ciphertext) == 0 {
			continue
		}
		if _, err := secrets.DecryptEnvelope(ciphertext, oidc.TransactionVerifierAAD(id, providerID)); err != nil {
			_ = transactionRows.Close()
			return fmt.Errorf("archived oidc transaction verifier failed authentication: %w", err)
		}
	}
	if err := transactionRows.Close(); err != nil {
		return fmt.Errorf("close archived oidc transaction rows: %w", err)
	}
	return nil
}

func isNoSuchTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}
