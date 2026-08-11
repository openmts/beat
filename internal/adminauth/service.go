package adminauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const (
	sessionIdleLifetime     = 2 * time.Hour
	sessionAbsoluteLifetime = 7 * 24 * time.Hour
	reauthenticationWindow  = 10 * time.Minute
)

type Service struct {
	config    ServiceConfig
	limiter   *RateLimiter
	dummyHash string
	now       func() time.Time
	random    io.Reader
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Store == nil || config.Secrets == nil {
		return nil, errors.New("administrator authentication dependencies are required")
	}
	if config.Passwords == nil {
		config.Passwords = NewPasswordHasher(DefaultPasswordParams(), rand.Reader)
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	dummyHash, err := config.Passwords.Hash("beat constant work dummy password")
	if err != nil {
		return nil, fmt.Errorf("create constant-work password hash: %w", err)
	}
	return &Service{config: config, limiter: NewRateLimiter(4096), dummyHash: dummyHash,
		now: config.Now, random: config.Random}, nil
}

func (service *Service) State(ctx context.Context) (AuthState, error) {
	count, err := service.config.Store.CountUsers(ctx)
	if err != nil {
		return AuthState{}, err
	}
	return AuthState{SetupRequired: count == 0}, nil
}

func (service *Service) LegacyAuthorize(ctx context.Context, token string) (bool, error) {
	state, err := service.State(ctx)
	if err != nil {
		return false, err
	}
	return state.SetupRequired && constantTokenEqual(token, service.config.LegacyToken), nil
}

func (service *Service) Logout(ctx context.Context, principal *model.AdminPrincipal) error {
	if err := service.config.Store.RevokeSession(ctx, principal.Session.ID, service.now()); err != nil {
		return err
	}
	return service.RecordAudit(ctx, AuditInput{Principal: principal, Action: "auth.logout",
		ResourceType: "session", ResourceID: principal.Session.ID, Outcome: model.AuditOutcomeSuccess})
}

func (service *Service) Bootstrap(ctx context.Context, request BootstrapRequest) (LoginResult, error) {
	state, err := service.State(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if !state.SetupRequired {
		return LoginResult{}, ErrBootstrapComplete
	}
	if !constantTokenEqual(request.BootstrapToken, service.config.LegacyToken) {
		return LoginResult{}, ErrBootstrapDenied
	}
	user, err := service.newBootstrapUser(request)
	if err != nil {
		return LoginResult{}, err
	}
	result, session, err := service.newSession(user, request.IPAddress, request.UserAgent)
	if err != nil {
		return LoginResult{}, err
	}
	if err := service.config.Store.Bootstrap(ctx, user, session); err != nil {
		return LoginResult{}, fmt.Errorf("bootstrap administrator: %w", err)
	}
	_ = service.RecordAudit(ctx, AuditInput{Principal: &result.Principal, Action: "auth.bootstrap",
		ResourceType: "administrator", ResourceID: user.ID, Outcome: model.AuditOutcomeSuccess,
		IPAddress: request.IPAddress, UserAgent: request.UserAgent})
	return result, nil
}

func (service *Service) Login(ctx context.Context, request LoginRequest) (LoginResult, error) {
	request.Username = model.NormalizeUsername(request.Username)
	keys := []string{"ip:" + request.IPAddress, "user:" + request.Username}
	now := service.now()
	if !service.limiter.Allowed(keys, now) {
		_ = service.RecordAudit(ctx, AuditInput{Action: "auth.login", ResourceType: "session",
			Outcome: model.AuditOutcomeFailure, IPAddress: request.IPAddress, UserAgent: request.UserAgent})
		return LoginResult{}, ErrLocked
	}
	user, err := service.config.Store.GetUserByUsername(ctx, request.Username)
	if err != nil {
		return LoginResult{}, err
	}
	if err := service.verifyLogin(user, request, now); err != nil {
		service.limiter.Failure(keys, now)
		_ = service.RecordAudit(ctx, AuditInput{Action: "auth.login", ResourceType: "session",
			Outcome: model.AuditOutcomeFailure, IPAddress: request.IPAddress, UserAgent: request.UserAgent})
		return LoginResult{}, err
	}
	result, session, err := service.newSession(user, request.IPAddress, request.UserAgent)
	if err != nil {
		return LoginResult{}, err
	}
	if err := service.config.Store.CreateSession(ctx, session); err != nil {
		return LoginResult{}, err
	}
	if err := service.config.Store.TouchUserLogin(ctx, user.ID, now); err != nil {
		_ = service.config.Store.RevokeSession(ctx, session.ID, now)
		return LoginResult{}, err
	}
	service.limiter.Success(keys)
	_ = service.RecordAudit(ctx, AuditInput{Principal: &result.Principal, Action: "auth.login",
		ResourceType: "session", ResourceID: session.ID, Outcome: model.AuditOutcomeSuccess,
		IPAddress: request.IPAddress, UserAgent: request.UserAgent})
	return result, nil
}

func (service *Service) Authenticate(ctx context.Context, rawToken string) (model.AdminPrincipal, error) {
	hash := sha256.Sum256([]byte(rawToken))
	session, err := service.config.Store.GetSessionByTokenHash(ctx, hash[:])
	if err != nil || session == nil || !session.Active(service.now()) {
		return model.AdminPrincipal{}, ErrSessionInvalid
	}
	user, err := service.config.Store.GetUserByID(ctx, session.UserID)
	if err != nil || user == nil || !user.Enabled {
		return model.AdminPrincipal{}, ErrSessionInvalid
	}
	now := service.now()
	idleExpiry := minTime(now.Add(sessionIdleLifetime), session.AbsoluteExpiresAt)
	if err := service.config.Store.TouchSession(ctx, session.ID, now, idleExpiry); err != nil {
		return model.AdminPrincipal{}, ErrSessionInvalid
	}
	session.LastActivityAt = now
	session.IdleExpiresAt = idleExpiry
	return model.AdminPrincipal{User: *user, Session: *session}, nil
}

func (service *Service) newBootstrapUser(request BootstrapRequest) (*model.AdminUser, error) {
	if err := model.ValidateAdminPassword(request.Password); err != nil {
		return nil, err
	}
	passwordHash, err := service.config.Passwords.Hash(request.Password)
	if err != nil {
		return nil, err
	}
	now := service.now()
	user := &model.AdminUser{ID: uuid.New().String(), Username: request.Username,
		DisplayName: request.DisplayName, Role: model.AdminRoleOwner, PasswordHash: passwordHash,
		Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	user.Normalize()
	if err := user.Validate(); err != nil {
		return nil, err
	}
	return user, nil
}

func (service *Service) newSession(
	user *model.AdminUser, ipAddress, userAgent string,
) (LoginResult, *model.AdminSession, error) {
	raw, hash, prefix, err := GenerateSessionToken(service.random)
	if err != nil {
		return LoginResult{}, nil, err
	}
	now := service.now()
	session := &model.AdminSession{ID: uuid.New().String(), UserID: user.ID, TokenHash: hash,
		TokenPrefix: prefix, CreatedAt: now, LastActivityAt: now,
		IdleExpiresAt: now.Add(sessionIdleLifetime), AbsoluteExpiresAt: now.Add(sessionAbsoluteLifetime),
		IPAddress: ipAddress, UserAgent: userAgent}
	principal := model.AdminPrincipal{User: *user, Session: *session}
	return LoginResult{Token: raw, Principal: principal}, session, nil
}

func (service *Service) verifyLogin(user *model.AdminUser, request LoginRequest, now time.Time) error {
	hash := service.dummyHash
	if user != nil {
		hash = user.PasswordHash
	}
	valid, err := service.config.Passwords.Verify(request.Password, hash)
	if err != nil || user == nil || !user.Enabled || !valid {
		return ErrInvalidCredentials
	}
	if !user.TOTPEnabled() {
		return nil
	}
	if request.TOTPCode == "" {
		return ErrTOTPRequired
	}
	valid, err = service.validateTOTP(user, request.TOTPCode, now)
	if err != nil || !valid {
		return ErrInvalidTOTP
	}
	return nil
}

func constantTokenEqual(actual, expected string) bool {
	actualHash := sha256.Sum256([]byte(actual))
	expectedHash := sha256.Sum256([]byte(expected))
	return expected != "" && subtle.ConstantTimeCompare(actualHash[:], expectedHash[:]) == 1
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
