package adminauth

import (
	"errors"
	"io"
	"time"

	"github.com/beat/backend/internal/model"
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
	ErrRecentAuthRequired = errors.New("recent authentication is required")
	ErrForbidden          = errors.New("administrator permission is required")
)

type ServiceConfig struct {
	Store       *store.AdminStore
	Secrets     *secretbox.Manager
	LegacyToken string
	Passwords   *PasswordHasher
	Random      io.Reader
	Now         func() time.Time
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
