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
	config         ServiceConfig
	security       *securityCounters
	limiter        *RateLimiter
	passwordBudget *authenticationBudget
	pendingTOTP    *pendingTOTPStore
	live           *liveSessionRegistry
	liveInterval   time.Duration
	dummyHash      string
	now            func() time.Time
	random         io.Reader
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
	return &Service{config: config, security: newSecurityCounters(), limiter: NewRateLimiter(4096), dummyHash: dummyHash,
		passwordBudget: newAuthenticationBudget(), pendingTOTP: newPendingTOTPStore(),
		live: &liveSessionRegistry{sessions: make(map[*liveSession]struct{})}, liveInterval: liveSessionCheckInterval,
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

// Now 返回服务时钟的当前 UTC 时间，供路由层做最近重认证判定。
func (service *Service) Now() time.Time {
	return service.now()
}

func (service *Service) Logout(ctx context.Context, principal *model.AdminPrincipal) error {
	if principal == nil {
		return ErrSessionInvalid
	}
	if err := service.commitSessionSecurity(ctx, *principal, nil); err != nil {
		return err
	}
	service.revokeLiveSession(principal.Session.ID)
	return nil
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
	release, err := service.admitPassword(ctx, "ip:"+request.IPAddress)
	if err != nil {
		return LoginResult{}, err
	}
	defer release()
	user, err := service.newBootstrapUser(request)
	if err != nil {
		return LoginResult{}, err
	}
	result, _, err := service.newSession(user, request.IPAddress, request.UserAgent)
	if err != nil {
		return LoginResult{}, err
	}
	if err := service.admitLocalSession(ctx, result, true); err != nil {
		return LoginResult{}, fmt.Errorf("bootstrap administrator: %w", err)
	}
	result.Principal.User.LastLoginAt = &result.Principal.Session.CreatedAt
	return result, nil
}

func (service *Service) Login(ctx context.Context, request LoginRequest) (LoginResult, error) {
	release, err := service.admitPassword(ctx, "ip:"+request.IPAddress)
	if err != nil {
		return LoginResult{}, err
	}
	defer release()
	request.Username = model.NormalizeUsername(request.Username)
	keys := []string{"ip:" + request.IPAddress, "user:" + request.Username}
	now := service.now()
	locked, err := service.loginLocked(ctx, request, now)
	if err != nil {
		return LoginResult{}, err
	}
	if locked || !service.limiter.Allowed(keys, now) {
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
		service.recordLoginFailure(ctx, loginFailureInput{Username: request.Username, IPAddress: request.IPAddress,
			UserAgent: request.UserAgent, Reason: failureReason(err), Now: now})
		return LoginResult{}, err
	}
	result, _, err := service.newSession(user, request.IPAddress, request.UserAgent)
	if err != nil {
		return LoginResult{}, err
	}
	if err := service.admitLocalSession(ctx, result, false); err != nil {
		return LoginResult{}, err
	}
	service.limiter.Success(keys)
	service.clearLoginThresholds(ctx, request.Username, request.IPAddress, now)
	result.Principal.User.LastLoginAt = &result.Principal.Session.CreatedAt
	return result, nil
}

func (service *Service) Authenticate(ctx context.Context, rawToken string) (model.AdminPrincipal, error) {
	hash := sha256.Sum256([]byte(rawToken))
	session, err := service.config.Store.GetSessionByTokenHash(ctx, hash[:])
	if err != nil {
		return model.AdminPrincipal{}, sessionStoreError("load session", err)
	}
	if session == nil || !session.Active(service.now()) {
		return model.AdminPrincipal{}, ErrSessionInvalid
	}
	user, err := service.config.Store.GetUserByID(ctx, session.UserID)
	if err != nil {
		return model.AdminPrincipal{}, sessionStoreError("load session user", err)
	}
	if user == nil || !user.Enabled {
		return model.AdminPrincipal{}, ErrSessionInvalid
	}
	now := service.now()
	idleExpiry := minTime(now.Add(sessionIdleLifetime), session.AbsoluteExpiresAt)
	if err := service.config.Store.TouchSession(ctx, session.ID, now, idleExpiry); err != nil {
		return model.AdminPrincipal{}, sessionStoreError("touch session", err)
	}
	session.LastActivityAt = now
	session.IdleExpiresAt = idleExpiry
	return model.AdminPrincipal{User: *user, Session: *session}, nil
}

// sessionStoreError 把存储故障包装成 ErrSessionStore 并保留原始原因：
// 路由层据此返回 503，运维日志里能直接看到底层错误（例如磁盘写满）。
func sessionStoreError(operation string, err error) error {
	return fmt.Errorf("%s: %w", operation, errors.Join(ErrSessionStore, err))
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
	return service.newSessionWithAttribution(user, ipAddress, userAgent,
		model.AuthMethodLocal, nil)
}

func (service *Service) newSessionWithAttribution(
	user *model.AdminUser, ipAddress, userAgent, method string, externalIdentityID *string,
) (LoginResult, *model.AdminSession, error) {
	raw, hash, prefix, err := GenerateSessionToken(service.random)
	if err != nil {
		return LoginResult{}, nil, err
	}
	now := service.now()
	session := &model.AdminSession{ID: uuid.New().String(), UserID: user.ID, TokenHash: hash,
		TokenPrefix: prefix, CreatedAt: now, LastActivityAt: now,
		IdleExpiresAt: now.Add(sessionIdleLifetime), AbsoluteExpiresAt: now.Add(sessionAbsoluteLifetime),
		IPAddress: ipAddress, UserAgent: userAgent,
		AuthenticationMethod: method, ExternalIdentityID: externalIdentityID}
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
