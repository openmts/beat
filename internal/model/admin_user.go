package model

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type AdminRole string

const (
	AdminRoleOwner AdminRole = "owner"
	AdminRoleAdmin AdminRole = "admin"
)

var adminUsernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,64}$`)

type AdminUser struct {
	ID                  string     `json:"id"`
	Username            string     `json:"username"`
	DisplayName         string     `json:"display_name"`
	Role                AdminRole  `json:"role"`
	PasswordHash        string     `json:"-"`
	Enabled             bool       `json:"enabled"`
	PasswordChangedAt   time.Time  `json:"password_changed_at"`
	LastLoginAt         *time.Time `json:"last_login_at"`
	TOTPSecretEncrypted []byte     `json:"-"`
	TOTPEnabledAt       *time.Time `json:"totp_enabled_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (user *AdminUser) Normalize() {
	user.Username = NormalizeUsername(user.Username)
	user.DisplayName = strings.TrimSpace(user.DisplayName)
}

func (user AdminUser) Validate() error {
	if !adminUsernamePattern.MatchString(user.Username) {
		return errors.New("username must contain 3 to 64 letters, digits, dots, underscores, or hyphens")
	}
	if utf8.RuneCountInString(user.DisplayName) < 1 || utf8.RuneCountInString(user.DisplayName) > 80 {
		return errors.New("display name must contain 1 to 80 characters")
	}
	if user.Role != AdminRoleOwner && user.Role != AdminRoleAdmin {
		return errors.New("administrator role is invalid")
	}
	return nil
}

func (user AdminUser) TOTPEnabled() bool {
	return user.TOTPEnabledAt != nil && len(user.TOTPSecretEncrypted) > 0
}

func (user AdminUser) String() string {
	encoded, err := json.Marshal(user)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func ValidateAdminPassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < 12 || length > 128 {
		return errors.New("password must contain 12 to 128 characters")
	}
	return nil
}
