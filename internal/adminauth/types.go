package adminauth

import (
	"errors"
	"io"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLocked             = errors.New("login temporarily locked")
	ErrTOTPRequired       = errors.New("TOTP code is required")
	ErrInvalidTOTP        = errors.New("TOTP code is invalid")
	ErrBootstrapComplete  = errors.New("administrator bootstrap is already complete")
	ErrBootstrapDenied    = errors.New("administrator bootstrap is not authorized")
	ErrSessionInvalid     = errors.New("administrator session is invalid")
	// ErrSessionStore 表示会话校验本身依赖的存储不可读或不可写；
	// 与「会话失效」区分开，路由层才能返回 503 而不是误导性的 401。
	ErrSessionStore       = errors.New("administrator session store is unavailable")
	ErrRecentAuthRequired = errors.New("recent authentication is required")
	ErrForbidden          = errors.New("administrator permission is required")
	ErrTOTPSetupExpired   = errors.New("authenticator setup is missing or expired; start setup again")
	ErrTOTPSetupCapacity  = errors.New("authenticator setup capacity reached; try again later")
)

type passwordOperations interface {
	Hash(string) (string, error)
	Verify(string, string) (bool, error)
}

type ServiceConfig struct {
	Store       *store.AdminStore
	Secrets     secretbox.ResourceService
	LegacyToken string
	// Queue 为登录安全事件提供原子出箱；为空时事件仍会记录并标记 unavailable。
	Queue     notificationqueue.BatchEnqueuer
	Passwords passwordOperations
	Random    io.Reader
	Now       func() time.Time
}

type AuthState struct {
	SetupRequired bool `json:"setup_required"`
}

type BootstrapRequest struct {
	BootstrapToken string
	Username       string
	DisplayName    string
	Password       string
	IPAddress      string
	UserAgent      string
}

type LoginRequest struct {
	Username  string
	Password  string
	TOTPCode  string
	IPAddress string
	UserAgent string
}

type LoginResult struct {
	Token     string               `json:"-"`
	Principal model.AdminPrincipal `json:"principal"`
}

type TOTPSetup struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

type CreateUserRequest struct {
	Username    string
	DisplayName string
	Role        model.AdminRole
	Password    string
}

type UpdateUserRequest struct {
	DisplayName string
	Role        model.AdminRole
	Enabled     bool
}

type AuditInput struct {
	RequestID    string
	Principal    *model.AdminPrincipal
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	IPAddress    string
	UserAgent    string
}
