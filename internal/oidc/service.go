package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

const (
	transactionLifetime = 5 * time.Minute
	maxReturnPathBytes  = 512
	oauthStateBytes     = 32
	nonceBytes          = 24
	verifierBytes       = 48
	bindingBytes        = 32
)

var (
	ErrProviderNotFound      = errors.New("oidc: provider not found")
	ErrProviderDisabled      = errors.New("oidc: provider is disabled")
	ErrTransactionInvalid    = errors.New("oidc: transaction is invalid")
	ErrTransactionExpired    = errors.New("oidc: transaction has expired")
	ErrCallbackFailed        = errors.New("oidc: callback failed")
	ErrIdentityNotLinked     = errors.New("oidc: identity is not linked")
	ErrIdentityAlreadyLinked = errors.New("oidc: identity is already linked to another user")
	ErrIdentityConflict      = errors.New("oidc: user already has this identity")
	ErrRecentAuthRequired    = errors.New("oidc: recent authentication is required")
	ErrIssuerImmutable       = errors.New("oidc: provider issuer is immutable")
	ErrInvalidProviderInput  = errors.New("oidc: invalid provider configuration")
	ErrProviderInUse         = errors.New("oidc: provider has linked identities")
	ErrSubjectInvalid        = errors.New("oidc: subject claim is invalid")
	ErrBootstrapNotSupported = errors.New("oidc: bootstrap does not support oidc")
	ErrIdentityNotFound      = errors.New("oidc: identity not found")
)

// BrowserBinding 是回调路径的浏览器绑定 cookie 值。
type BrowserBinding struct {
	Value  string
	Path   string
	MaxAge int
}

// BeginResult 是发起登录或链接事务的结果。
type BeginResult struct {
	AuthorizationURL string
	BrowserBinding   BrowserBinding
}

// IdentityClaims 是登录后从已验证 ID 令牌提取的最小展示字段。
type IdentityClaims struct {
	Subject           string
	Email             string
	EmailVerified     bool
	PreferredUsername string
	DisplayName       string
}

// Service 实现 OIDC 登录、链接与提供方管理的完整流程。
type Service struct {
	store   *store.OIDCStore
	users   *store.AdminStore
	secrets *secretbox.Manager
	auth    *adminauth.Service
	client  *ProviderClient
	now     func() time.Time
	random  io.Reader
}

// NewService 构造 OIDC 服务。auth 仅用于 OIDC 会话创建与审计。
func NewService(
	store *store.OIDCStore,
	users *store.AdminStore,
	secrets *secretbox.Manager,
	auth *adminauth.Service,
	client *ProviderClient,
	now func() time.Time,
	random io.Reader,
) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if random == nil {
		random = rand.Reader
	}
	return &Service{store: store, users: users, secrets: secrets, auth: auth,
		client: client, now: now, random: random}
}

// ListPublicProviders 返回启用提供方的最小公开视图。
func (service *Service) ListPublicProviders(ctx context.Context) ([]model.OIDCProviderPublic, error) {
	providers, err := service.store.ListEnabledProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.OIDCProviderPublic, 0, len(providers))
	for _, provider := range providers {
		out = append(out, model.OIDCProviderPublic{
			ID: provider.ID, Slug: provider.Slug, DisplayName: provider.DisplayName,
		})
	}
	return out, nil
}

// BeginLogin 发起一次登录事务。
func (service *Service) BeginLogin(
	ctx context.Context, providerID, returnPath string, schemeHost, ipAddress, userAgent string,
) (BeginResult, error) {
	provider, err := service.requireEnabledProvider(ctx, providerID)
	if err != nil {
		return BeginResult{}, err
	}
	if err := validateReturnPath(returnPath); err != nil {
		return BeginResult{}, err
	}
	return service.begin(ctx, provider, returnPath, "", model.OIDCPurposeLogin, schemeHost, ipAddress, userAgent)
}

// BeginLink 发起一次链接事务；要求当前用户最近已本地重认证。
func (service *Service) BeginLink(
	ctx context.Context, principal *model.AdminPrincipal, providerID, returnPath string,
	schemeHost, ipAddress, userAgent string,
) (BeginResult, error) {
	if !principal.Session.RecentlyAuthenticated(service.now()) {
		return BeginResult{}, ErrRecentAuthRequired
	}
	provider, err := service.requireEnabledProvider(ctx, providerID)
	if err != nil {
		return BeginResult{}, err
	}
	if err := validateReturnPath(returnPath); err != nil {
		return BeginResult{}, err
	}
	return service.begin(ctx, provider, returnPath, principal.User.ID, model.OIDCPurposeLink, schemeHost, ipAddress, userAgent)
}

func (service *Service) begin(
	ctx context.Context, provider *model.OIDCProvider, returnPath, userID, purpose, schemeHost, ipAddress, userAgent string,
) (BeginResult, error) {
	state, err := randomBase64(oauthStateBytes, service.random)
	if err != nil {
		return BeginResult{}, err
	}
	nonce, err := randomBase64(nonceBytes, service.random)
	if err != nil {
		return BeginResult{}, err
	}
	verifier, err := randomBase64(verifierBytes, service.random)
	if err != nil {
		return BeginResult{}, err
	}
	binding, err := randomBase64(bindingBytes, service.random)
	if err != nil {
		return BeginResult{}, err
	}
	challenge, err := s256Challenge(verifier)
	if err != nil {
		return BeginResult{}, err
	}

	transactionID := uuid.NewString()
	encryptedVerifier, err := service.secrets.EncryptEnvelope([]byte(verifier),
		TransactionVerifierAAD(transactionID, provider.ID))
	if err != nil {
		return BeginResult{}, fmt.Errorf("encrypt pkce verifier: %w", err)
	}
	redirectURI := strings.TrimRight(schemeHost, "/") + "/api/v1/auth/oidc/callback"
	now := service.now()
	transaction := &model.OIDCTransaction{
		ID: transactionID, StateHash: hashBytes([]byte(state)),
		BrowserBindingHash: hashBytes([]byte(binding)), NonceHash: hashBytes([]byte(nonce)),
		CodeVerifierEncrypted: encryptedVerifier, ProviderID: provider.ID,
		ProviderConfigurationVersion: provider.ConfigurationVersion, Purpose: purpose,
		UserID: userID, RedirectURI: redirectURI, ReturnPath: returnPath,
		CreatedAt: now, ExpiresAt: now.Add(transactionLifetime),
	}
	if err := service.store.CreateTransaction(ctx, transaction); err != nil {
		return BeginResult{}, err
	}

	info, err := service.client.Discover(ctx, provider.IssuerURL)
	if err != nil {
		return BeginResult{}, fmt.Errorf("discover oidc provider: %w", err)
	}
	authorizationURL := service.client.AuthorizationURL(
		info, provider.ClientID, redirectURI, strings.Join(provider.Scopes, " "),
		state, nonce, challenge)
	return BeginResult{
		AuthorizationURL: authorizationURL,
		BrowserBinding: BrowserBinding{Value: binding, Path: "/api/v1/auth/oidc/callback",
			MaxAge: int(transactionLifetime.Seconds())},
	}, nil
}

// HandleCallback 消费事务、校验授权码并完成登录或链接。
func (service *Service) HandleCallback(
	ctx context.Context, state, code, browserBinding, ipAddress, userAgent string,
) (string, string, error) {
	if state == "" || code == "" || browserBinding == "" {
		return "", "", ErrCallbackFailed
	}
	stateHash := hashBytes([]byte(state))
	transaction, err := service.store.ConsumeTransaction(ctx, stateHash, service.now())
	if err != nil {
		return "", "", err
	}
	if transaction == nil {
		return "", "", ErrCallbackFailed
	}
	if service.now().After(transaction.ExpiresAt) {
		return "", "", ErrTransactionExpired
	}
	if !constantTimeEqual(transaction.BrowserBindingHash, hashBytes([]byte(browserBinding))) {
		return "", "", ErrCallbackFailed
	}
	provider, err := service.store.GetProviderByID(ctx, transaction.ProviderID)
	if err != nil || provider == nil {
		return "", "", ErrProviderNotFound
	}
	if !provider.Enabled || provider.ConfigurationVersion != transaction.ProviderConfigurationVersion {
		return "", "", ErrProviderDisabled
	}
	verifierBytesRaw, err := service.secrets.DecryptEnvelope(transaction.CodeVerifierEncrypted,
		TransactionVerifierAAD(transaction.ID, provider.ID))
	if err != nil {
		return "", "", fmt.Errorf("decrypt pkce verifier: %w", err)
	}
	clientSecret, err := service.providerSecret(provider)
	if err != nil {
		return "", "", err
	}
	info, err := service.client.Discover(ctx, provider.IssuerURL)
	if err != nil {
		return "", "", err
	}
	rawIDToken, err := service.client.ExchangeCode(
		ctx, info, provider.ClientID, clientSecret, transaction.RedirectURI,
		code, string(verifierBytesRaw), provider.Scopes)
	if err != nil {
		return "", "", err
	}
	claims, err := service.verifyIDToken(ctx, info, provider, transaction, rawIDToken)
	if err != nil {
		return "", "", err
	}

	switch transaction.Purpose {
	case model.OIDCPurposeLogin:
		return service.completeLogin(ctx, provider, transaction, claims, ipAddress, userAgent)
	case model.OIDCPurposeLink:
		return service.completeLink(ctx, provider, transaction, claims, ipAddress, userAgent)
	default:
		return "", "", ErrCallbackFailed
	}
}

func (service *Service) completeLogin(
	ctx context.Context, provider *model.OIDCProvider, transaction *model.OIDCTransaction,
	claims IdentityClaims, ipAddress, userAgent string,
) (string, string, error) {
	identity, err := service.store.GetIdentityByProviderSubject(ctx, provider.ID, claims.Subject)
	if err != nil || identity == nil {
		return "", "", ErrIdentityNotLinked
	}
	user, err := service.authUser(ctx, identity.UserID)
	if err != nil {
		return "", "", err
	}
	if err := service.store.TouchIdentityLogin(ctx, identity.ID, service.now()); err != nil {
		return "", "", err
	}
	result, err := service.auth.CreateOIDCSession(ctx, user.ID, identity.ID, ipAddress, userAgent)
	if err != nil {
		return "", "", err
	}
	_ = service.auth.RecordAudit(ctx, adminauth.AuditInput{
		RequestID: "", Principal: &result.Principal, Action: "auth.oidc.login",
		ResourceType: "provider", ResourceID: provider.ID, Outcome: model.AuditOutcomeSuccess,
		IPAddress: ipAddress, UserAgent: userAgent,
	})
	return transaction.ReturnPath, result.Token, nil
}

func (service *Service) completeLink(
	ctx context.Context, provider *model.OIDCProvider, transaction *model.OIDCTransaction,
	claims IdentityClaims, ipAddress, userAgent string,
) (string, string, error) {
	user, err := service.authUser(ctx, transaction.UserID)
	if err != nil {
		return "", "", err
	}
	existing, err := service.store.GetIdentityByProviderSubject(ctx, provider.ID, claims.Subject)
	if err != nil {
		return "", "", err
	}
	if existing != nil {
		return "", "", ErrIdentityAlreadyLinked
	}
	now := service.now()
	identity := &model.ExternalIdentity{
		ID: uuid.NewString(), ProviderID: provider.ID, UserID: user.ID, Subject: claims.Subject,
		Email: claims.Email, EmailVerified: claims.EmailVerified,
		PreferredUsername: claims.PreferredUsername, DisplayName: claims.DisplayName,
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.store.LinkIdentity(ctx, identity); err != nil {
		return "", "", ErrIdentityConflict
	}
	_ = service.auth.RecordAudit(ctx, adminauth.AuditInput{
		Principal: &model.AdminPrincipal{User: *user}, Action: "auth.oidc.link",
		ResourceType: "identity", ResourceID: identity.ID, Outcome: model.AuditOutcomeSuccess,
		IPAddress: ipAddress, UserAgent: userAgent,
	})
	return transaction.ReturnPath, "", nil
}

func (service *Service) verifyIDToken(
	ctx context.Context, info *ProviderInfo, provider *model.OIDCProvider,
	transaction *model.OIDCTransaction, rawIDToken string,
) (IdentityClaims, error) {
	verifier := service.client.Verifier(ctx, info, provider.ClientID)
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return IdentityClaims{}, fmt.Errorf("verify id token: %w", err)
	}
	if !constantTimeEqual(transaction.NonceHash, hashBytes([]byte(idToken.Nonce))) {
		return IdentityClaims{}, errors.New("id token nonce mismatch")
	}
	var claims struct {
		Subject           string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return IdentityClaims{}, err
	}
	if claims.Subject == "" || len(claims.Subject) > 1024 {
		return IdentityClaims{}, ErrSubjectInvalid
	}
	return IdentityClaims{
		Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified,
		PreferredUsername: claims.PreferredUsername, DisplayName: claims.Name,
	}, nil
}

// ListIdentitiesForUser 列出当前用户的已链接身份（含提供方名称）。
func (service *Service) ListIdentitiesForUser(ctx context.Context, userID string) ([]model.ExternalIdentity, error) {
	return service.store.ListIdentitiesByUser(ctx, userID)
}

// Unlink 解除当前用户的身份链接；本地凭据始终保留，因此无需最后凭据校验。
func (service *Service) Unlink(
	ctx context.Context, principal *model.AdminPrincipal, identityID, ipAddress, userAgent string,
) error {
	if !principal.Session.RecentlyAuthenticated(service.now()) {
		return ErrRecentAuthRequired
	}
	identity, err := service.store.GetIdentityByID(ctx, identityID)
	if err != nil {
		return err
	}
	if identity == nil || identity.UserID != principal.User.ID {
		return ErrIdentityNotFound
	}
	removed, err := service.store.UnlinkIdentity(ctx, identityID, principal.User.ID)
	if err != nil {
		return err
	}
	if !removed {
		return ErrIdentityNotFound
	}
	_ = service.auth.RecordAudit(ctx, adminauth.AuditInput{
		Principal: principal, Action: "auth.oidc.unlink",
		ResourceType: "identity", ResourceID: identityID, Outcome: model.AuditOutcomeSuccess,
		IPAddress: ipAddress, UserAgent: userAgent,
	})
	return nil
}

// ListProviders 返回全部提供方（owner 路由）。
func (service *Service) ListProviders(ctx context.Context) ([]model.OIDCProvider, error) {
	return service.store.ListProviders(ctx)
}

// CreateProvider 校验并创建提供方；issuer 不可变、发现元数据必须有效。
func (service *Service) CreateProvider(
	ctx context.Context, input ProviderInput, ipAddress, userAgent string,
) (*model.OIDCProvider, error) {
	if err := service.client.policy.ValidateIssuer(input.IssuerURL); err != nil {
		return nil, ErrInvalidProviderInput
	}
	if err := validateScopes(input.Scopes); err != nil {
		return nil, ErrInvalidProviderInput
	}
	if input.ClientSecret == "" {
		return nil, ErrInvalidProviderInput
	}
	if _, err := service.client.Discover(ctx, input.IssuerURL); err != nil {
		return nil, ErrInvalidProviderInput
	}
	now := service.now()
	provider := &model.OIDCProvider{
		ID: uuid.NewString(), Slug: input.Slug, DisplayName: input.DisplayName,
		IssuerURL: normalizeIssuer(input.IssuerURL), ClientID: input.ClientID,
		Scopes: input.Scopes, Enabled: input.Enabled, ConfigurationVersion: 1,
		SortOrder: input.SortOrder, CreatedAt: now, UpdatedAt: now,
	}
	secret, err := service.secrets.EncryptEnvelope([]byte(input.ClientSecret),
		ProviderSecretAAD(provider.ID, 1))
	if err != nil {
		return nil, fmt.Errorf("encrypt provider secret: %w", err)
	}
	provider.ClientSecretEncrypted = secret
	if err := service.store.CreateProvider(ctx, provider); err != nil {
		return nil, err
	}
	_ = service.auth.RecordAudit(ctx, adminauth.AuditInput{
		Action: "auth.oidc.provider.create", ResourceType: "provider", ResourceID: provider.ID,
		Outcome: model.AuditOutcomeSuccess, IPAddress: ipAddress, UserAgent: userAgent,
	})
	return provider, nil
}

// UpdateProvider 更新提供方；issuer 不可变，凭据/客户端变更使存量事务失效。
func (service *Service) UpdateProvider(
	ctx context.Context, id string, input ProviderInput, ipAddress, userAgent string,
) (*model.OIDCProvider, error) {
	provider, err := service.store.GetProviderByID(ctx, id)
	if err != nil || provider == nil {
		return nil, ErrProviderNotFound
	}
	if input.IssuerURL != "" && normalizeIssuer(input.IssuerURL) != provider.IssuerURL {
		return nil, ErrIssuerImmutable
	}
	if err := validateScopes(input.Scopes); err != nil {
		return nil, ErrInvalidProviderInput
	}
	version := provider.ConfigurationVersion
	secretChanged := input.ClientSecret != ""
	if secretChanged {
		if _, err := service.client.Discover(ctx, provider.IssuerURL); err != nil {
			return nil, ErrInvalidProviderInput
		}
		version++
	}
	wasEnabled := provider.Enabled
	provider.DisplayName = input.DisplayName
	provider.ClientID = input.ClientID
	provider.Scopes = input.Scopes
	provider.Enabled = input.Enabled
	provider.SortOrder = input.SortOrder
	provider.ConfigurationVersion = version
	provider.UpdatedAt = service.now()
	if secretChanged {
		encrypted, err := service.secrets.EncryptEnvelope([]byte(input.ClientSecret),
			ProviderSecretAAD(provider.ID, version))
		if err != nil {
			return nil, fmt.Errorf("encrypt provider secret: %w", err)
		}
		provider.ClientSecretEncrypted = encrypted
	}
	if err := service.store.UpdateProvider(ctx, provider); err != nil {
		return nil, err
	}
	action := "auth.oidc.provider.update"
	if wasEnabled != provider.Enabled {
		action = "auth.oidc.provider.enable"
		if !provider.Enabled {
			action = "auth.oidc.provider.disable"
		}
	}
	_ = service.auth.RecordAudit(ctx, adminauth.AuditInput{
		Action: action, ResourceType: "provider", ResourceID: provider.ID,
		Outcome: model.AuditOutcomeSuccess, IPAddress: ipAddress, UserAgent: userAgent,
	})
	return provider, nil
}

// DeleteProvider 仅允许在无身份引用时删除。
func (service *Service) DeleteProvider(
	ctx context.Context, id, ipAddress, userAgent string,
) error {
	if err := service.store.DeleteProvider(ctx, id); err != nil {
		if errors.Is(err, store.ErrOIDCProviderHasIdentities) {
			return ErrProviderInUse
		}
		return err
	}
	_ = service.auth.RecordAudit(ctx, adminauth.AuditInput{
		Action: "auth.oidc.provider.delete", ResourceType: "provider", ResourceID: id,
		Outcome: model.AuditOutcomeSuccess, IPAddress: ipAddress, UserAgent: userAgent,
	})
	return nil
}

// ProviderInput 是提供方创建/更新的规范化输入。
type ProviderInput struct {
	Slug         string
	DisplayName  string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Scopes       []string
	Enabled      bool
	SortOrder    int
}

func (service *Service) requireEnabledProvider(ctx context.Context, id string) (*model.OIDCProvider, error) {
	provider, err := service.store.GetProviderByID(ctx, id)
	if err != nil || provider == nil {
		return nil, ErrProviderNotFound
	}
	if !provider.Enabled {
		return nil, ErrProviderDisabled
	}
	return provider, nil
}

func (service *Service) providerSecret(provider *model.OIDCProvider) (string, error) {
	if len(provider.ClientSecretEncrypted) == 0 {
		return "", nil
	}
	plaintext, err := service.secrets.DecryptEnvelope(provider.ClientSecretEncrypted,
		ProviderSecretAAD(provider.ID, provider.ConfigurationVersion))
	if err != nil {
		return "", fmt.Errorf("decrypt provider secret: %w", err)
	}
	return string(plaintext), nil
}

// ProviderSecretAAD 返回提供方客户端密钥的资源绑定 AAD；备份校验复用同一构造。
func ProviderSecretAAD(providerID string, configurationVersion int) secretbox.ResourceAAD {
	return secretbox.ResourceAAD{
		Domain: "beat", Namespace: "oidc_provider", RowID: providerID,
		Field: "client_secret", Revision: fmt.Sprintf("%d", configurationVersion),
		PayloadVersion: "v1",
	}
}

// TransactionVerifierAAD 返回 PKCE 验证器的资源绑定 AAD；备份校验复用同一构造。
func TransactionVerifierAAD(transactionID, providerID string) secretbox.ResourceAAD {
	return secretbox.ResourceAAD{
		Domain: "beat", Namespace: "oidc_transaction", RowID: transactionID,
		Field: "pkce_verifier", Revision: providerID, PayloadVersion: "v1",
	}
}

func (service *Service) authUser(ctx context.Context, userID string) (*model.AdminUser, error) {
	user, err := service.users.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return nil, ErrIdentityNotLinked
	}
	if !user.Enabled {
		return nil, ErrIdentityNotLinked
	}
	return user, nil
}

func validateReturnPath(path string) error {
	if path == "" || len(path) > maxReturnPathBytes {
		return ErrInvalidProviderInput
	}
	if !strings.HasPrefix(path, "/admin") {
		return ErrInvalidProviderInput
	}
	parsed, err := url.Parse(path)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" {
		return ErrInvalidProviderInput
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return ErrInvalidProviderInput
		}
	}
	return nil
}

func validateScopes(scopes []string) error {
	if len(scopes) == 0 {
		return errors.New("scopes are required")
	}
	for _, scope := range scopes {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n\"") {
			return errors.New("invalid scope")
		}
	}
	for _, scope := range scopes {
		if scope == "openid" {
			return nil
		}
	}
	return errors.New("scopes must include openid")
}

func hashBytes(raw []byte) []byte {
	digest := sha256.Sum256(raw)
	return digest[:]
}

func constantTimeEqual(left, right []byte) bool {
	return subtle.ConstantTimeCompare(left, right) == 1
}

func randomBase64(byteCount int, random io.Reader) (string, error) {
	raw := make([]byte, byteCount)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func s256Challenge(verifier string) (string, error) {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

// CallbackRedirectPath 供 handler 校验返回值路径为固定内部路径。
func CallbackRedirectPath(path string) error {
	if err := validateReturnPath(path); err != nil {
		return err
	}
	return nil
}
