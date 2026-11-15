package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"beat/internal/platform/sqlite"
)

const (
	CredentialsKey  = "admin_credentials"
	DefaultUsername = "admin"
	DefaultPassword = "admin"
	SessionTTL      = 12 * time.Hour

	passwordSaltBytes  = 16
	passwordKeyBytes   = 32
	passwordIterations = 120_000
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidUsername    = errors.New("invalid username")
	ErrPasswordTooShort   = errors.New("password is too short")
	ErrPasswordTooLong    = errors.New("password is too long")
	ErrNoChanges          = errors.New("credentials are unchanged")
)

type Credentials struct {
	Username   string    `json:"username"`
	Salt       string    `json:"salt"`
	Hash       string    `json:"hash"`
	Iterations int       `json:"iterations"`
	Version    int64     `json:"version"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Session struct {
	Token     string
	Username  string
	Version   int64
	CSRFToken string
	ExpiresAt time.Time
}

type Manager struct {
	store *sqlite.Store
	now   func() time.Time
}

func NewManager(ctx context.Context, store *sqlite.Store) (*Manager, error) {
	manager := &Manager{store: store, now: time.Now}
	if err := manager.ensureDefault(ctx); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) ensureDefault(ctx context.Context) error {
	_, ok, err := m.store.GetSetting(ctx, CredentialsKey)
	if err != nil {
		return fmt.Errorf("read admin credentials: %w", err)
	}
	if ok {
		credentials, err := loadCredentials(ctx, m.store)
		if err != nil {
			return err
		}
		if _, _, err := decodeHash(credentials); err != nil {
			return fmt.Errorf("validate admin credentials: %w", err)
		}
		return nil
	}
	_, err = saveNewCredentials(ctx, m.store, DefaultUsername, DefaultPassword, 1, m.now())
	return err
}

func (m *Manager) Authenticate(ctx context.Context, username, password string) (Credentials, bool, error) {
	credentials, err := loadCredentials(ctx, m.store)
	if err != nil {
		return Credentials{}, false, err
	}
	validPassword := verifyPassword(credentials, password)
	validUsername := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(credentials.Username)) == 1
	valid := validPassword && validUsername
	return credentials, valid, nil
}

func (m *Manager) UpdateCredentials(ctx context.Context, currentPassword, username, newPassword string) (Credentials, error) {
	credentials, err := loadCredentials(ctx, m.store)
	if err != nil {
		return Credentials{}, err
	}
	if !verifyPassword(credentials, currentPassword) {
		return Credentials{}, ErrInvalidCredentials
	}

	username = strings.TrimSpace(username)
	if username == "" {
		username = credentials.Username
	}
	if err := validateUsername(username); err != nil {
		return Credentials{}, err
	}
	if newPassword != "" {
		if err := validatePassword(newPassword); err != nil {
			return Credentials{}, err
		}
	}
	if username == credentials.Username && newPassword == "" {
		return Credentials{}, ErrNoChanges
	}

	now := m.now().UTC()
	updated := credentials
	updated.Username = username
	updated.Version = credentials.Version + 1
	updated.UpdatedAt = now
	if newPassword != "" {
		salt, hash, err := hashPassword(newPassword)
		if err != nil {
			return Credentials{}, err
		}
		updated.Salt = salt
		updated.Hash = hash
		updated.Iterations = passwordIterations
	}
	if err := saveCredentials(ctx, m.store, updated); err != nil {
		return Credentials{}, err
	}
	if err := m.store.DeleteAllAdminSessions(ctx); err != nil {
		return Credentials{}, fmt.Errorf("invalidate admin sessions: %w", err)
	}
	return updated, nil
}

func (m *Manager) Reset(ctx context.Context) (Credentials, error) {
	version := int64(1)
	if current, err := loadCredentials(ctx, m.store); err == nil {
		version = current.Version + 1
	}
	credentials, err := saveNewCredentials(ctx, m.store, DefaultUsername, DefaultPassword, version, m.now())
	if err != nil {
		return Credentials{}, err
	}
	if err := m.store.DeleteAllAdminSessions(ctx); err != nil {
		return Credentials{}, fmt.Errorf("invalidate admin sessions: %w", err)
	}
	return credentials, nil
}

func (m *Manager) IssueSession(username string, version int64) (Session, error) {
	token, err := randomToken()
	if err != nil {
		return Session{}, fmt.Errorf("generate session token: %w", err)
	}
	csrfToken, err := randomToken()
	if err != nil {
		return Session{}, fmt.Errorf("generate CSRF token: %w", err)
	}
	now := m.now().UTC()
	session := Session{
		Token:     token,
		Username:  username,
		Version:   version,
		CSRFToken: csrfToken,
		ExpiresAt: now.Add(SessionTTL),
	}
	if err := m.store.CreateAdminSession(context.Background(), hashToken(token), username, version, csrfToken, now, session.ExpiresAt); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (m *Manager) ValidateSession(ctx context.Context, token string) (Session, bool, error) {
	if strings.TrimSpace(token) == "" {
		return Session{}, false, nil
	}
	hash := hashToken(token)
	record, ok, err := m.store.GetAdminSession(ctx, hash)
	if err != nil {
		return Session{}, false, err
	}
	if !ok {
		return Session{}, false, nil
	}
	now := m.now().UTC()
	session := Session{
		Token:     token,
		Username:  record.Username,
		Version:   record.CredentialVersion,
		CSRFToken: record.CSRFToken,
		ExpiresAt: record.ExpiresAt,
	}
	if now.After(session.ExpiresAt) {
		_ = m.store.DeleteAdminSession(ctx, hash)
		return Session{}, false, nil
	}

	credentials, err := loadCredentials(ctx, m.store)
	if err != nil {
		return Session{}, false, err
	}
	if credentials.Version != session.Version || credentials.Username != session.Username {
		m.RevokeSession(token)
		return Session{}, false, nil
	}
	if now.Sub(record.LastSeenAt) >= 5*time.Minute {
		_ = m.store.TouchAdminSession(ctx, hash, now)
	}
	return session, true, nil
}

func (m *Manager) RevokeSession(token string) {
	if token == "" {
		return
	}
	_ = m.store.DeleteAdminSession(context.Background(), hashToken(token))
}

func ResetCredentials(ctx context.Context, store *sqlite.Store) (Credentials, error) {
	version := int64(1)
	if current, err := loadCredentials(ctx, store); err == nil {
		version = current.Version + 1
	}
	credentials, err := saveNewCredentials(ctx, store, DefaultUsername, DefaultPassword, version, time.Now().UTC())
	if err != nil {
		return Credentials{}, err
	}
	if err := store.DeleteAllAdminSessions(ctx); err != nil {
		return Credentials{}, fmt.Errorf("invalidate admin sessions: %w", err)
	}
	return credentials, nil
}

func randomToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(tokenBytes), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func loadCredentials(ctx context.Context, store *sqlite.Store) (Credentials, error) {
	raw, ok, err := store.GetSetting(ctx, CredentialsKey)
	if err != nil {
		return Credentials{}, fmt.Errorf("read admin credentials: %w", err)
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return Credentials{}, errors.New("admin credentials are not configured")
	}
	var credentials Credentials
	if err := json.Unmarshal([]byte(raw), &credentials); err != nil {
		return Credentials{}, fmt.Errorf("decode admin credentials: %w", err)
	}
	if err := validateStoredCredentials(credentials); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

func saveNewCredentials(ctx context.Context, store *sqlite.Store, username, password string, version int64, now time.Time) (Credentials, error) {
	salt, hash, err := hashPassword(password)
	if err != nil {
		return Credentials{}, err
	}
	credentials := Credentials{
		Username:   username,
		Salt:       salt,
		Hash:       hash,
		Iterations: passwordIterations,
		Version:    version,
		UpdatedAt:  now.UTC(),
	}
	if err := saveCredentials(ctx, store, credentials); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

func saveCredentials(ctx context.Context, store *sqlite.Store, credentials Credentials) error {
	payload, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("encode admin credentials: %w", err)
	}
	if err := store.SetSetting(ctx, CredentialsKey, string(payload)); err != nil {
		return fmt.Errorf("save admin credentials: %w", err)
	}
	return nil
}

func validateStoredCredentials(credentials Credentials) error {
	if err := validateUsername(credentials.Username); err != nil {
		return err
	}
	if credentials.Version < 1 {
		return errors.New("invalid credential version")
	}
	if credentials.Iterations < 10_000 || credentials.Iterations > 1_000_000 {
		return errors.New("invalid password iteration count")
	}
	if _, _, err := decodeHash(credentials); err != nil {
		return err
	}
	return nil
}

func validateUsername(username string) error {
	if username == "" || utf8.RuneCountInString(username) > 64 {
		return ErrInvalidUsername
	}
	for _, character := range username {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return ErrInvalidUsername
		}
	}
	return nil
}

func validatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < 8 {
		return ErrPasswordTooShort
	}
	if length > 128 {
		return ErrPasswordTooLong
	}
	return nil
}

func hashPassword(password string) (string, string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := pbkdf2SHA256([]byte(password), salt, passwordIterations, passwordKeyBytes)
	return base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(hash), nil
}

func verifyPassword(credentials Credentials, password string) bool {
	salt, expected, err := decodeHash(credentials)
	if err != nil {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, credentials.Iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func decodeHash(credentials Credentials) ([]byte, []byte, error) {
	salt, err := base64.StdEncoding.DecodeString(credentials.Salt)
	if err != nil || len(salt) < 8 {
		return nil, nil, errors.New("invalid password salt")
	}
	hash, err := base64.StdEncoding.DecodeString(credentials.Hash)
	if err != nil || len(hash) < 16 {
		return nil, nil, errors.New("invalid password hash")
	}
	return salt, hash, nil
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	result := make([]byte, 0, blocks*hashLength)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for iteration := 1; iteration < iterations; iteration++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for index := range t {
				t[index] ^= u[index]
			}
		}
		result = append(result, t...)
	}
	return result[:keyLength]
}
