package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

var (
	ErrOIDCProviderNotFound      = errors.New("store: oidc provider not found")
	ErrOIDCTransactionNotFound   = errors.New("store: oidc transaction not found")
	ErrOIDCProviderHasIdentities = errors.New("store: oidc provider still has linked identities")
	ErrExternalIdentityNotFound  = errors.New("store: external identity not found")
	ErrExternalIdentityConflict  = errors.New("store: external identity already linked")
)

type OIDCStore struct {
	db *sql.DB
}

func NewOIDCStore(db *sql.DB) *OIDCStore {
	return &OIDCStore{db: db}
}

const oidcProviderColumns = `id, slug, display_name, issuer_url, client_id,
	client_secret_encrypted, scopes_json, enabled, configuration_version,
	sort_order, created_at, updated_at`

func scanOIDCProvider(scanner sessionScanner) (*model.OIDCProvider, error) {
	var provider model.OIDCProvider
	var scopes string
	err := scanner.Scan(&provider.ID, &provider.Slug, &provider.DisplayName,
		&provider.IssuerURL, &provider.ClientID, &provider.ClientSecretEncrypted,
		&scopes, &provider.Enabled, &provider.ConfigurationVersion, &provider.SortOrder,
		&provider.CreatedAt, &provider.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan oidc provider: %w", err)
	}
	provider.Scopes = decodeScopesJSON(scopes)
	return &provider, nil
}

func (store *OIDCStore) CreateProvider(ctx context.Context, provider *model.OIDCProvider) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO admin_oidc_providers (
		id, slug, display_name, issuer_url, client_id, client_secret_encrypted,
		scopes_json, enabled, configuration_version, sort_order, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider.ID, provider.Slug, provider.DisplayName, provider.IssuerURL,
		provider.ClientID, provider.ClientSecretEncrypted, encodeScopesJSON(provider.Scopes),
		provider.Enabled, provider.ConfigurationVersion, provider.SortOrder,
		provider.CreatedAt, provider.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create oidc provider: %w", err)
	}
	return nil
}

func (store *OIDCStore) UpdateProvider(ctx context.Context, provider *model.OIDCProvider) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_oidc_providers SET
		display_name = ?, client_id = ?, client_secret_encrypted = ?, scopes_json = ?,
		enabled = ?, configuration_version = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		provider.DisplayName, provider.ClientID, provider.ClientSecretEncrypted,
		encodeScopesJSON(provider.Scopes), provider.Enabled, provider.ConfigurationVersion,
		provider.SortOrder, provider.UpdatedAt, provider.ID)
	if err != nil {
		return fmt.Errorf("update oidc provider: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update oidc provider rows affected: %w", err)
	}
	if affected == 0 {
		return ErrOIDCProviderNotFound
	}
	return nil
}

// IncrementProviderConfigurationVersion 使既有登录/链接事务失效。
func (store *OIDCStore) IncrementProviderConfigurationVersion(ctx context.Context, id string) error {
	result, err := store.db.ExecContext(ctx, `UPDATE admin_oidc_providers SET
		configuration_version = configuration_version + 1, updated_at = ?
		WHERE id = ?`, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("increment oidc provider configuration version: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("increment oidc provider rows affected: %w", err)
	}
	if affected == 0 {
		return ErrOIDCProviderNotFound
	}
	return nil
}

func (store *OIDCStore) GetProviderByID(ctx context.Context, id string) (*model.OIDCProvider, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+oidcProviderColumns+
		" FROM admin_oidc_providers WHERE id = ?", id)
	return scanOIDCProvider(row)
}

func (store *OIDCStore) GetProviderByIssuer(ctx context.Context, issuer string) (*model.OIDCProvider, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+oidcProviderColumns+
		" FROM admin_oidc_providers WHERE issuer_url = ?", issuer)
	return scanOIDCProvider(row)
}

func (store *OIDCStore) ListProviders(ctx context.Context) ([]model.OIDCProvider, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+oidcProviderColumns+
		" FROM admin_oidc_providers ORDER BY sort_order, display_name")
	if err != nil {
		return nil, fmt.Errorf("list oidc providers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	providers := []model.OIDCProvider{}
	for rows.Next() {
		provider, scanErr := scanOIDCProvider(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		providers = append(providers, *provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oidc providers: %w", err)
	}
	return providers, nil
}

func (store *OIDCStore) ListEnabledProviders(ctx context.Context) ([]model.OIDCProvider, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+oidcProviderColumns+
		" FROM admin_oidc_providers WHERE enabled = 1 ORDER BY sort_order, display_name")
	if err != nil {
		return nil, fmt.Errorf("list enabled oidc providers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	providers := []model.OIDCProvider{}
	for rows.Next() {
		provider, scanErr := scanOIDCProvider(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		providers = append(providers, *provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled oidc providers: %w", err)
	}
	return providers, nil
}

// DeleteProvider 仅当没有身份引用该提供方时才允许删除。
func (store *OIDCStore) DeleteProvider(ctx context.Context, id string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete oidc provider: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM admin_external_identities WHERE provider_id = ?", id,
	).Scan(&count); err != nil {
		return fmt.Errorf("count provider identities: %w", err)
	}
	if count > 0 {
		return ErrOIDCProviderHasIdentities
	}
	result, err := tx.ExecContext(ctx,
		"DELETE FROM admin_oidc_providers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete oidc provider: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete oidc provider rows affected: %w", err)
	}
	if affected == 0 {
		return ErrOIDCProviderNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete oidc provider: %w", err)
	}
	return nil
}

// --- 外部身份 ---

const externalIdentityColumns = `id, provider_id, user_id, subject, email,
	email_verified, preferred_username, display_name, last_login_at, created_at, updated_at`

func scanExternalIdentity(scanner sessionScanner) (*model.ExternalIdentity, error) {
	var identity model.ExternalIdentity
	err := scanner.Scan(&identity.ID, &identity.ProviderID, &identity.UserID,
		&identity.Subject, &identity.Email, &identity.EmailVerified,
		&identity.PreferredUsername, &identity.DisplayName, &identity.LastLoginAt,
		&identity.CreatedAt, &identity.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan external identity: %w", err)
	}
	return &identity, nil
}

func (store *OIDCStore) LinkIdentity(ctx context.Context, identity *model.ExternalIdentity) error {
	if _, err := store.db.ExecContext(ctx, `INSERT INTO admin_external_identities (
		id, provider_id, user_id, subject, email, email_verified, preferred_username,
		display_name, last_login_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		identity.ID, identity.ProviderID, identity.UserID, identity.Subject,
		identity.Email, identity.EmailVerified, identity.PreferredUsername,
		identity.DisplayName, identity.LastLoginAt, identity.CreatedAt, identity.UpdatedAt,
	); err != nil {
		return fmt.Errorf("link external identity: %w", err)
	}
	return nil
}

func (store *OIDCStore) UnlinkIdentity(ctx context.Context, id, userID string) (bool, error) {
	result, err := store.db.ExecContext(ctx,
		"DELETE FROM admin_external_identities WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return false, fmt.Errorf("unlink external identity: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("unlink external identity rows affected: %w", err)
	}
	return affected == 1, nil
}

func (store *OIDCStore) GetIdentityByProviderSubject(
	ctx context.Context, providerID, subject string,
) (*model.ExternalIdentity, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+externalIdentityColumns+
		" FROM admin_external_identities WHERE provider_id = ? AND subject = ?", providerID, subject)
	return scanExternalIdentity(row)
}

func (store *OIDCStore) GetIdentityByID(ctx context.Context, id string) (*model.ExternalIdentity, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+externalIdentityColumns+
		" FROM admin_external_identities WHERE id = ?", id)
	return scanExternalIdentity(row)
}

func (store *OIDCStore) ListIdentitiesByUser(ctx context.Context, userID string) ([]model.ExternalIdentity, error) {
	const joinedColumns = `e.id, e.provider_id, e.user_id, e.subject, e.email,
		e.email_verified, e.preferred_username, e.display_name, e.last_login_at,
		e.created_at, e.updated_at`
	rows, err := store.db.QueryContext(ctx, `SELECT `+joinedColumns+
		`, p.display_name FROM admin_external_identities e JOIN admin_oidc_providers p
		ON p.id = e.provider_id WHERE e.user_id = ? ORDER BY e.created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list external identities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	identities := []model.ExternalIdentity{}
	for rows.Next() {
		var identity model.ExternalIdentity
		var providerName string
		err := rows.Scan(&identity.ID, &identity.ProviderID, &identity.UserID,
			&identity.Subject, &identity.Email, &identity.EmailVerified,
			&identity.PreferredUsername, &identity.DisplayName, &identity.LastLoginAt,
			&identity.CreatedAt, &identity.UpdatedAt, &providerName)
		if err != nil {
			return nil, fmt.Errorf("scan external identity with provider: %w", err)
		}
		identity.ProviderName = providerName
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external identities: %w", err)
	}
	return identities, nil
}

func (store *OIDCStore) TouchIdentityLogin(ctx context.Context, id string, at time.Time) error {
	if _, err := store.db.ExecContext(ctx, `UPDATE admin_external_identities SET
		last_login_at = ?, updated_at = ? WHERE id = ?`, at, at, id); err != nil {
		return fmt.Errorf("touch external identity login: %w", err)
	}
	return nil
}

// --- 登录/链接事务 ---

func (store *OIDCStore) CreateTransaction(ctx context.Context, transaction *model.OIDCTransaction) error {
	if _, err := store.db.ExecContext(ctx, `INSERT INTO admin_oidc_transactions (
		id, state_hash, browser_binding_hash, nonce_hash, code_verifier_encrypted,
		provider_id, provider_configuration_version, purpose, user_id, redirect_uri,
		return_path, created_at, expires_at, consumed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		transaction.ID, transaction.StateHash, transaction.BrowserBindingHash,
		transaction.NonceHash, transaction.CodeVerifierEncrypted, transaction.ProviderID,
		transaction.ProviderConfigurationVersion, transaction.Purpose, transaction.UserID,
		transaction.RedirectURI, transaction.ReturnPath, transaction.CreatedAt,
		transaction.ExpiresAt, transaction.ConsumedAt,
	); err != nil {
		return fmt.Errorf("create oidc transaction: %w", err)
	}
	return nil
}

// ConsumeTransaction 原子地标记未过期事务为已消费；仅一次调用会成功。
func (store *OIDCStore) ConsumeTransaction(
	ctx context.Context, stateHash []byte, now time.Time,
) (*model.OIDCTransaction, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin consume oidc transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	row := tx.QueryRowContext(ctx, `SELECT id, state_hash, browser_binding_hash, nonce_hash,
		code_verifier_encrypted, provider_id, provider_configuration_version, purpose,
		user_id, redirect_uri, return_path, created_at, expires_at, consumed_at
		FROM admin_oidc_transactions WHERE state_hash = ?`, stateHash)
	transaction, err := scanOIDCTransaction(row)
	if err != nil {
		return nil, err
	}
	if transaction == nil || transaction.Expired(now) || transaction.ConsumedAt != nil {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE admin_oidc_transactions SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL",
		now, transaction.ID); err != nil {
		return nil, fmt.Errorf("consume oidc transaction: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit consume oidc transaction: %w", err)
	}
	transaction.ConsumedAt = &now
	return transaction, nil
}

func scanOIDCTransaction(scanner sessionScanner) (*model.OIDCTransaction, error) {
	var transaction model.OIDCTransaction
	var userID sql.NullString
	err := scanner.Scan(&transaction.ID, &transaction.StateHash, &transaction.BrowserBindingHash,
		&transaction.NonceHash, &transaction.CodeVerifierEncrypted, &transaction.ProviderID,
		&transaction.ProviderConfigurationVersion, &transaction.Purpose, &userID,
		&transaction.RedirectURI, &transaction.ReturnPath, &transaction.CreatedAt,
		&transaction.ExpiresAt, &transaction.ConsumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan oidc transaction: %w", err)
	}
	transaction.UserID = userID.String
	return &transaction, nil
}

func (store *OIDCStore) DeleteExpiredTransactions(ctx context.Context, now time.Time) (int64, error) {
	result, err := store.db.ExecContext(ctx,
		"DELETE FROM admin_oidc_transactions WHERE expires_at <= ? OR consumed_at IS NOT NULL",
		now.Add(-time.Hour))
	if err != nil {
		return 0, fmt.Errorf("delete expired oidc transactions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired oidc transactions rows affected: %w", err)
	}
	return count, nil
}

func encodeScopesJSON(scopes []string) string {
	if len(scopes) == 0 {
		return `["openid"]`
	}
	raw, err := json.Marshal(scopes)
	if err != nil {
		return `["openid"]`
	}
	return string(raw)
}

func decodeScopesJSON(raw string) []string {
	scopes := []string{}
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil || len(scopes) == 0 {
		return []string{"openid"}
	}
	return scopes
}
