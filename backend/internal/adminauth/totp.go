package adminauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/beat/backend/internal/model"
)

var totpValidationOptions = totp.ValidateOpts{
	Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
}

func (service *Service) BeginTOTP(
	ctx context.Context, principal *model.AdminPrincipal,
) (TOTPSetup, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: "Beat", AccountName: principal.User.Username, Rand: service.random,
	})
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("generate TOTP secret: %w", err)
	}
	encrypted, err := service.config.Secrets.Encrypt([]byte(key.Secret()))
	if err != nil {
		return TOTPSetup{}, err
	}
	if err := service.config.Store.UpdateTOTP(ctx, principal.User.ID, encrypted, nil); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: key.Secret(), URI: key.URL()}, nil
}

func (service *Service) EnableTOTP(
	ctx context.Context, principal *model.AdminPrincipal, code string,
) error {
	user, err := service.config.Store.GetUserByID(ctx, principal.User.ID)
	if err != nil || user == nil || len(user.TOTPSecretEncrypted) == 0 {
		return errors.New("TOTP setup has not been started")
	}
	valid, err := service.validateTOTP(user, code, service.now())
	if err != nil || !valid {
		return ErrInvalidTOTP
	}
	now := service.now()
	return service.config.Store.UpdateTOTP(ctx, user.ID, user.TOTPSecretEncrypted, &now)
}

func (service *Service) Reauthenticate(
	ctx context.Context, principal *model.AdminPrincipal, password, code string,
) error {
	user, err := service.config.Store.GetUserByID(ctx, principal.User.ID)
	if err != nil || user == nil || !user.Enabled {
		return ErrInvalidCredentials
	}
	valid, err := service.config.Passwords.Verify(password, user.PasswordHash)
	if err != nil || !valid {
		return ErrInvalidCredentials
	}
	if user.TOTPEnabled() {
		valid, err = service.validateTOTP(user, code, service.now())
		if err != nil || !valid {
			return ErrInvalidTOTP
		}
	}
	until := service.now().Add(reauthenticationWindow)
	if err := service.config.Store.MarkSessionReauthenticated(ctx, principal.Session.ID, until); err != nil {
		return err
	}
	principal.Session.ReauthenticatedUntil = &until
	return service.RecordAudit(ctx, AuditInput{Principal: principal, Action: "auth.reauthenticate",
		ResourceType: "session", ResourceID: principal.Session.ID, Outcome: model.AuditOutcomeSuccess})
}

func (service *Service) validateTOTP(user *model.AdminUser, code string, now time.Time) (bool, error) {
	secret, err := service.config.Secrets.Decrypt(user.TOTPSecretEncrypted)
	if err != nil {
		return false, err
	}
	return totp.ValidateCustom(code, string(secret), now, totpValidationOptions)
}
