package backup

import (
	"database/sql"

	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/secretbox"
)

type archivedSecret struct {
	ciphertext []byte
	aad        secretbox.ResourceAAD
	legacy     bool
	required   bool
}

type archivedSecretSource struct {
	name  string
	query string
	scan  func(*sql.Rows) (archivedSecret, error)
}

func archivedSecretSources() []archivedSecretSource {
	return []archivedSecretSource{
		{name: "TOTP secrets", query: "SELECT totp_secret_encrypted, totp_enabled_at FROM admin_users", scan: scanArchivedTOTP},
		{name: "OIDC providers", query: "SELECT id, configuration_version, client_secret_encrypted FROM admin_oidc_providers", scan: scanArchivedProvider},
		{name: "OIDC transactions", query: "SELECT id, provider_id, code_verifier_encrypted FROM admin_oidc_transactions", scan: scanArchivedTransaction},
	}
}

func scanArchivedTOTP(rows *sql.Rows) (archivedSecret, error) {
	var secret archivedSecret
	var enabled sql.NullTime
	err := rows.Scan(&secret.ciphertext, &enabled)
	secret.legacy, secret.required = true, enabled.Valid
	return secret, err
}

func scanArchivedProvider(rows *sql.Rows) (archivedSecret, error) {
	var id string
	var revision int
	var ciphertext []byte
	err := rows.Scan(&id, &revision, &ciphertext)
	return archivedSecret{ciphertext: ciphertext, aad: oidc.ProviderSecretAAD(id, revision)}, err
}

func scanArchivedTransaction(rows *sql.Rows) (archivedSecret, error) {
	var id, providerID string
	var ciphertext []byte
	err := rows.Scan(&id, &providerID, &ciphertext)
	return archivedSecret{ciphertext: ciphertext, aad: oidc.TransactionVerifierAAD(id, providerID)}, err
}
