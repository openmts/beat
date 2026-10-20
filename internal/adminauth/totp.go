package adminauth

import (
	"context"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

var totpValidationOptions = totp.ValidateOpts{
	Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
}

func (service *Service) BeginTOTP(
	ctx context.Context, principal *model.AdminPrincipal,
) (TOTPSetup, error) {
	service.pendingTOTP.mu.Lock()
	defer service.pendingTOTP.mu.Unlock()
	snapshot, err := service.selfTOTPSnapshot(ctx, principal)
	if err != nil {
		return TOTPSetup{}, err
	}
	service.pendingTOTP.prune(service.now())
	if _, exists := service.pendingTOTP.entries[principal.Session.ID]; !exists && len(service.pendingTOTP.entries) >= maximumPendingTOTP {
		return TOTPSetup{}, ErrTOTPSetupCapacity
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: "Beat", AccountName: snapshot.User.Username, Rand: service.random,
	})
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("generate TOTP secret: %w", err)
	}
	if _, err := service.commitSelfSecurity(ctx, service.selfSecurityChange(snapshot, store.AdministratorTOTPBegin, snapshot.User.ID)); err != nil {
		return TOTPSetup{}, err
	}
	if previous, exists := service.pendingTOTP.entries[principal.Session.ID]; exists {
		clear(previous.secret)
	}
	service.pendingTOTP.entries[principal.Session.ID] = pendingTOTP{
		userID: snapshot.User.ID, secret: []byte(key.Secret()), previousSecret: snapshot.User.TOTPSecretEncrypted,
		previousEnabledAt: snapshot.User.TOTPEnabledAt, expiresAt: service.now().Add(pendingTOTPLifetime),
	}
	return TOTPSetup{Secret: key.Secret(), URI: key.URL()}, nil
}

func (service *Service) EnableTOTP(
	ctx context.Context, principal *model.AdminPrincipal, code string,
) error {
	service.pendingTOTP.mu.Lock()
	defer service.pendingTOTP.mu.Unlock()
	if _, err := service.totpUser(ctx, principal); err != nil {
		return err
	}
	service.pendingTOTP.prune(service.now())
	pending, exists := service.pendingTOTP.entries[principal.Session.ID]
	if !exists || pending.userID != principal.User.ID {
		return ErrTOTPSetupExpired
	}
	valid, err := totp.ValidateCustom(code, string(pending.secret), service.now(), totpValidationOptions)
	if err != nil || !valid {
		pending.failures++
		service.pendingTOTP.entries[principal.Session.ID] = pending
		if pending.failures >= maximumPendingTOTPFailures {
			clear(pending.secret)
			delete(service.pendingTOTP.entries, principal.Session.ID)
		}
		return ErrInvalidTOTP
	}
	return service.activateTOTP(ctx, principal, pending)
}

func (service *Service) activateTOTP(ctx context.Context, principal *model.AdminPrincipal, pending pendingTOTP) error {
	snapshot, err := service.selfTOTPSnapshot(ctx, principal)
	if err != nil {
		return err
	}
	err = service.config.Secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		ciphertext, err := access.Seal(pending.secret, secretbox.TOTPSecretAAD(principal.User.ID))
		if err != nil {
			return err
		}
		change := service.selfSecurityChange(snapshot, store.AdministratorTOTPEnable, snapshot.User.ID)
		change.Secret, change.PreviousSecret, change.PreviousEnabledAt = ciphertext, pending.previousSecret, pending.previousEnabledAt
		_, err = service.commitSelfSecurity(ctx, change)
		return err
	})
	if err != nil {
		return err
	}
	service.pendingTOTP.clearUser(principal.User.ID)
	return nil
}

func (service *Service) Reauthenticate(
	ctx context.Context, principal *model.AdminPrincipal, password, code string,
) error {
	if principal == nil {
		return ErrSessionInvalid
	}
	release, err := service.admitPassword(ctx, "session:"+principal.Session.ID)
	if err != nil {
		return err
	}
	defer release()
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
	snapshot := *principal
	if user.Role != principal.User.Role {
		return ErrSessionInvalid
	}
	snapshot.User = *user
	if err := service.commitSessionSecurity(ctx, snapshot, &until); err != nil {
		return err
	}
	principal.User = *user
	principal.Session.ReauthenticatedUntil = &until
	return nil
}

func (service *Service) validateTOTP(user *model.AdminUser, code string, now time.Time) (bool, error) {
	secret, err := service.config.Secrets.OpenResource(context.Background(), user.TOTPSecretEncrypted, secretbox.TOTPSecretAAD(user.ID))
	if err != nil {
		return false, err
	}
	defer clear(secret)
	return totp.ValidateCustom(code, string(secret), now, totpValidationOptions)
}
