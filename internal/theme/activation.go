package theme

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (service *Service) Activate(ctx context.Context, id, actorID string) (*model.ThemePackage, error) {
	release, err := service.mutations.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	pkg, err := service.store.GetPackageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if pkg == nil || pkg.State != model.ThemeStateInstalled {
		return nil, ErrContentUnavailable
	}
	if err := service.verifyInstalledPackage(ctx, pkg); err != nil {
		return nil, err
	}
	if err := service.writeActivation(ctx, &model.ThemeActivation{PackageID: id, ActorID: actorID, Reason: model.ThemeActivationReason}); err != nil {
		return nil, err
	}
	return pkg, nil
}

func (service *Service) Rollback(ctx context.Context, actorID string) (string, error) {
	release, err := service.mutations.enter(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	current, err := service.store.CurrentActivation(ctx)
	if err != nil {
		return "", err
	}
	target, err := service.rollbackTarget(ctx, currentPackage(current))
	if err != nil {
		return "", err
	}
	if err := service.writeActivation(ctx, &model.ThemeActivation{PackageID: target, ActorID: actorID, Reason: model.ThemeRollbackReason}); err != nil {
		return "", err
	}
	if target == "" {
		return "built-in", nil
	}
	return target, nil
}

func (service *Service) rollbackTarget(ctx context.Context, currentID string) (string, error) {
	ids, err := service.store.HealthyRollbackCandidates(ctx, currentID)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		pkg, err := service.store.GetPackageByID(ctx, id)
		if err != nil {
			return "", err
		}
		if pkg != nil {
			verifyErr := service.verifyInstalledPackage(ctx, pkg)
			if verifyErr == nil {
				return id, nil
			}
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (service *Service) Fallback(ctx context.Context, actorID, category string) error {
	release, err := service.mutations.enter(ctx)
	if err != nil {
		return err
	}
	defer release()
	return service.writeActivation(ctx, &model.ThemeActivation{ActorID: actorID, Reason: model.ThemeFallbackReason})
}

func (service *Service) writeActivation(ctx context.Context, activation *model.ThemeActivation) error {
	activation.ID, activation.ActivatedAt = uuid.NewString(), service.now()
	err := service.store.ActivatePackage(ctx, activation, activation.ActivatedAt)
	if errors.Is(err, store.ErrThemePackageNotFound) {
		return ErrContentUnavailable
	}
	return err
}

func (service *Service) verifyInstalledPackage(ctx context.Context, pkg *model.ThemePackage) error {
	raw, _, err := service.Export(ctx, pkg.ID)
	if err != nil {
		return err
	}
	validated, err := ValidateArchive(raw)
	if err != nil {
		return ErrContentUnavailable
	}
	manifest := validated.Manifest
	if manifest.ID != pkg.ThemeID || manifest.Version != pkg.Version || manifest.Runtime != pkg.Runtime {
		return ErrContentUnavailable
	}
	if err := service.validateCompatibility(manifest); err != nil {
		return err
	}
	for _, file := range manifest.Files {
		if _, err := service.ReadAsset(ctx, pkg.ArchiveDigest, file.Path); err != nil {
			return err
		}
	}
	_, err = service.describeStoredPackage(ctx, pkg)
	return err
}
