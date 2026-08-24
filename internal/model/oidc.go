package model

import "time"

// OIDC 认证方法与会话归因常量。
const (
	AuthMethodLocal = "local"
	AuthMethodOIDC  = "oidc"

	OIDCPurposeLogin = "login"
	OIDCPurposeLink  = "link"
)

// OIDCProvider 是一个 OIDC 提供方配置。SecretEncrypted 永远不通过 API 返回。
type OIDCProvider struct {
	ID                    string    `json:"id"`
	Slug                  string    `json:"slug"`
	DisplayName           string    `json:"display_name"`
	IssuerURL             string    `json:"issuer_url"`
	ClientID              string    `json:"client_id"`
	ClientSecretEncrypted []byte    `json:"-"`
	Scopes                []string  `json:"scopes"`
	Enabled               bool      `json:"enabled"`
	ConfigurationVersion  int       `json:"configuration_version"`
	SortOrder             int       `json:"sort_order"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// ClientSecretConfigured 报告是否已配置客户端密钥，供响应展示使用。
func (provider OIDCProvider) ClientSecretConfigured() bool {
	return len(provider.ClientSecretEncrypted) > 0
}

// OIDCProviderPublic 是未认证路由暴露的最小提供方视图。
type OIDCProviderPublic struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

// OIDCTransaction 是一次登录或链接事务；仅存哈希与加密的 PKCE 验证器。
type OIDCTransaction struct {
	ID                           string
	StateHash                    []byte
	BrowserBindingHash           []byte
	NonceHash                    []byte
	CodeVerifierEncrypted        []byte
	ProviderID                   string
	ProviderConfigurationVersion int
	Purpose                      string
	UserID                       string
	RedirectURI                  string
	ReturnPath                   string
	CreatedAt                    time.Time
	ExpiresAt                    time.Time
	ConsumedAt                   *time.Time
}

// Expired 报告事务是否已超过有效期。
func (transaction OIDCTransaction) Expired(now time.Time) bool {
	return now.After(transaction.ExpiresAt)
}

// ExternalIdentity 将已验证的 OIDC 主题绑定到本地管理员。
type ExternalIdentity struct {
	ID                string    `json:"id"`
	ProviderID        string    `json:"provider_id"`
	UserID            string    `json:"-"`
	Subject           string    `json:"-"`
	Email             string    `json:"email"`
	EmailVerified     bool      `json:"email_verified"`
	PreferredUsername string    `json:"preferred_username,omitempty"`
	DisplayName       string    `json:"display_name,omitempty"`
	LastLoginAt       time.Time `json:"last_login_at"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	ProviderName      string    `json:"provider_name,omitempty"`
}
