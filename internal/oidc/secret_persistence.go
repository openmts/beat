package oidc

import (
	"context"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

func (service *Service) persistTransaction(ctx context.Context, transaction *model.OIDCTransaction, verifier string) error {
	return service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		ciphertext, err := access.Seal([]byte(verifier), TransactionVerifierAAD(transaction.ID, transaction.ProviderID))
		if err != nil {
			return err
		}
		transaction.CodeVerifierEncrypted = ciphertext
		return service.store.CreateTransaction(ctx, transaction)
	})
}

func (service *Service) persistNewProvider(ctx context.Context, provider *model.OIDCProvider, secret string) error {
	return service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		ciphertext, err := access.Seal([]byte(secret), ProviderSecretAAD(provider.ID, provider.ConfigurationVersion))
		if err != nil {
			return err
		}
		provider.ClientSecretEncrypted = ciphertext
		return service.store.CreateProvider(ctx, provider)
	})
}

func (service *Service) persistUpdatedProvider(ctx context.Context, provider *model.OIDCProvider, replacement string) error {
	return service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		current, err := service.store.GetProviderByID(ctx, provider.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrProviderNotFound
		}
		previousVersion := provider.ConfigurationVersion
		if replacement != "" {
			previousVersion--
		}
		if previousVersion != current.ConfigurationVersion {
			return ErrInvalidProviderInput
		}
		plaintext := []byte(replacement)
		if replacement == "" && len(current.ClientSecretEncrypted) > 0 {
			plaintext, err = access.Open(current.ClientSecretEncrypted, ProviderSecretAAD(current.ID, current.ConfigurationVersion))
			if err != nil {
				return err
			}
		}
		defer clear(plaintext)
		provider.ClientSecretEncrypted = nil
		if len(plaintext) > 0 {
			provider.ClientSecretEncrypted, err = access.Seal(plaintext, ProviderSecretAAD(provider.ID, provider.ConfigurationVersion))
			if err != nil {
				return err
			}
		}
		return service.store.UpdateProvider(ctx, provider)
	})
}
