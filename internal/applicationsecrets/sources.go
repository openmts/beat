package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/beat/backend/internal/secretbox"
)

type secretResource struct {
	id                          string
	ciphertext, legacyPlaintext []byte
	aad                         secretbox.ResourceAAD
	required                    bool
	invalid                     bool
}

type resourceSource struct {
	name, query, update string
	scan                func(*sql.Rows) (secretResource, error)
}

func resourceSources(legacySSH bool) []resourceSource {
	sshQuery := resourceQuery("ssh_keys", "id, "+boundedColumn("private_key_encrypted")+", ''", oversizedColumn("private_key_encrypted"))
	sshUpdate := "UPDATE ssh_keys SET private_key_encrypted = ? WHERE id = ? AND private_key_encrypted IS ?"
	if legacySSH {
		sshQuery = resourceQuery("ssh_keys", "id, "+boundedColumn("private_key_encrypted")+", "+boundedColumn("private_key"), oversizedColumn("private_key_encrypted")+" OR "+oversizedColumn("private_key"))
		sshUpdate = "UPDATE ssh_keys SET private_key_encrypted = ?, private_key = '' WHERE id = ? AND private_key_encrypted IS ?"
	}
	return []resourceSource{
		{name: "totp", query: resourceQuery("admin_users", "id, "+boundedColumn("totp_secret_encrypted")+", totp_enabled_at IS NOT NULL", oversizedColumn("totp_secret_encrypted")),
			update: "UPDATE admin_users SET totp_secret_encrypted = ? WHERE id = ? AND totp_secret_encrypted IS ?", scan: scanTOTPResource},
		{name: "oidc_provider", query: resourceQuery("admin_oidc_providers", "id, configuration_version, "+boundedColumn("client_secret_encrypted"), oversizedColumn("client_secret_encrypted")),
			update: "UPDATE admin_oidc_providers SET client_secret_encrypted = ? WHERE id = ? AND client_secret_encrypted IS ?", scan: scanProviderResource},
		{name: "oidc_transaction", query: resourceQuery("admin_oidc_transactions", "id, provider_id, "+boundedColumn("code_verifier_encrypted"), oversizedColumn("code_verifier_encrypted")),
			update: "UPDATE admin_oidc_transactions SET code_verifier_encrypted = ? WHERE id = ? AND code_verifier_encrypted IS ?", scan: scanTransactionResource},
		{name: "ssh", query: sshQuery, update: sshUpdate, scan: scanSSHResource},
	}
}

func scanTOTPResource(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	err := rows.Scan(&resource.id, &resource.ciphertext, &resource.required, &resource.invalid)
	resource.aad = secretbox.TOTPSecretAAD(resource.id)
	return resource, err
}

func scanProviderResource(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	var version int
	err := rows.Scan(&resource.id, &version, &resource.ciphertext, &resource.invalid)
	resource.aad = secretbox.OIDCProviderAAD(resource.id, version)
	return resource, err
}

func scanTransactionResource(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	var providerID string
	err := rows.Scan(&resource.id, &providerID, &resource.ciphertext, &resource.invalid)
	resource.aad = secretbox.OIDCTransactionAAD(resource.id, providerID)
	resource.required = true
	return resource, err
}

func scanSSHResource(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	err := rows.Scan(&resource.id, &resource.ciphertext, &resource.legacyPlaintext, &resource.invalid)
	resource.aad = secretbox.SSHPrivateKeyAAD(resource.id)
	return resource, err
}

func (source resourceSource) readBatch(ctx context.Context, database databaseReader, cursor string) (resources []secretResource, resultErr error) {
	rows, err := database.QueryContext(ctx, source.query, cursor, cursor, batchSize)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		resource, err := source.scan(rows)
		if err != nil {
			return nil, err
		}
		if resource.id == "" || resource.invalid || len(resource.ciphertext) > maximumResourceCiphertextBytes || len(resource.legacyPlaintext) > secretbox.MaximumSecretBytes {
			return nil, ErrCiphertext
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

const maximumResourceCiphertextBytes = secretbox.MaximumSecretBytes + 128

// 表名和字段名只由上面的固定注册表提供；WHERE 参数始终绑定。
func resourceQuery(table, columns, invalid string) string {
	return "SELECT " + columns + ", " + invalid + " FROM " + table + " WHERE (id > ? OR ? = '') ORDER BY id LIMIT ?"
}

func boundedColumn(column string) string {
	return "CASE WHEN " + oversizedColumn(column) + " THEN NULL ELSE " + column + " END"
}

func oversizedColumn(column string) string {
	limit := maximumResourceCiphertextBytes
	if !strings.HasSuffix(column, "_encrypted") {
		limit = secretbox.MaximumSecretBytes
	}
	return fmt.Sprintf("COALESCE(length(CAST(%s AS BLOB)),0) > %d", column, limit)
}

func hasLegacySSH(ctx context.Context, database databaseReader) (bool, error) {
	var count int
	err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('ssh_keys') WHERE name = 'private_key'").Scan(&count)
	return count != 0, err
}
