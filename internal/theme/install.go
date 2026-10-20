package theme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// Install 校验并安装一个主题包（不激活）。
func (service *Service) Install(
	ctx context.Context, raw []byte, sourceType, sourceID, actorID string,
) (*model.ThemePackage, error) {
	validated, err := ValidateArchive(raw)
	if err != nil {
		return nil, err
	}
	release, err := service.mutations.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	existing, err := service.store.GetPackageByDigest(ctx, validated.Digest)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.State != model.ThemeStateDeleted {
		return nil, ErrPackageConflict
	}
	if err := service.validateCompatibility(validated.Manifest); err != nil {
		return nil, err
	}
	return service.installValidated(ctx, validated, installationSource{raw: raw, kind: sourceType, id: sourceID, actor: actorID, existing: existing})
}

type installationSource struct {
	raw      []byte
	kind     string
	id       string
	actor    string
	existing *model.ThemePackage
}

func (service *Service) installValidated(ctx context.Context, validated *ValidatedPackage, source installationSource) (*model.ThemePackage, error) {
	staging, err := os.MkdirTemp(filepath.Join(service.root, "staging"), "theme-")
	if err != nil {
		return nil, fmt.Errorf("create theme staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := os.Chmod(staging, 0o700); err != nil {
		return nil, fmt.Errorf("secure theme staging directory: %w", err)
	}
	if err := service.stageArchive(staging, validated, source.raw); err != nil {
		return nil, err
	}

	pkg, err := service.packageRecord(validated, source)
	if err != nil {
		return nil, err
	}
	if source.existing != nil {
		if err := service.prepareRestoredPackage(pkg, source.existing); err != nil {
			return nil, err
		}
	}
	return service.commitStagedPackage(ctx, staging, installationRecord{pkg: pkg, restoring: source.existing != nil})
}

type installationRecord struct {
	pkg       *model.ThemePackage
	restoring bool
}

func (service *Service) prepareRestoredPackage(pkg, existing *model.ThemePackage) error {
	if pkg.ThemeID != existing.ThemeID || pkg.Version != existing.Version || pkg.Runtime != existing.Runtime {
		return ErrPackageConflict
	}
	manifest, err := parseManifest([]byte(pkg.ManifestJSON))
	if err != nil {
		return err
	}
	if _, err := ParseThemeConfiguration([]byte(existing.ConfigurationJSON), manifest.Configuration); err != nil {
		return err
	}
	pkg.ID, pkg.ConfigurationJSON = existing.ID, existing.ConfigurationJSON
	pkg.SourceType, pkg.SourceID = existing.SourceType, existing.SourceID
	pkg.CreatorID, pkg.CreatedAt = existing.CreatorID, existing.CreatedAt
	return service.clearDeletedContent(pkg.ArchiveDigest)
}

func (service *Service) commitStagedPackage(ctx context.Context, staging string, record installationRecord) (*model.ThemePackage, error) {
	packageDir := filepath.Join(service.root, "packages", record.pkg.ArchiveDigest)
	if err := os.Rename(staging, packageDir); err != nil {
		return nil, fmt.Errorf("commit theme package: %w", err)
	}
	if err := errors.Join(syncThemeDirectory(filepath.Dir(packageDir)), syncThemeDirectory(filepath.Dir(staging))); err != nil {
		return nil, errors.Join(err, service.clearDeletedContent(record.pkg.ArchiveDigest))
	}
	var err error
	if record.restoring {
		err = service.store.RestorePackage(ctx, record.pkg)
	} else {
		err = service.store.CreatePackage(ctx, record.pkg)
	}
	if err != nil {
		return nil, errors.Join(err, service.clearDeletedContent(record.pkg.ArchiveDigest))
	}
	return record.pkg, nil
}

func (service *Service) packageRecord(validated *ValidatedPackage, source installationSource) (*model.ThemePackage, error) {
	now := service.now()
	manifestJSON, err := json.Marshal(validated.Manifest)
	if err != nil {
		return nil, fmt.Errorf("encode theme manifest: %w", err)
	}
	pkg := &model.ThemePackage{
		ID: uuid.NewString(), ThemeID: validated.Manifest.ID, Name: validated.Manifest.Name,
		Version: validated.Manifest.Version, Author: validated.Manifest.Author,
		Description: validated.Manifest.Description, License: validated.Manifest.License,
		Runtime: validated.Manifest.Runtime, ManifestSchema: validated.Manifest.SchemaVersion,
		MinimumBeatVersion: validated.Manifest.MinimumBeatVersion,
		MaximumBeatVersion: validated.Manifest.MaximumBeatVersion,
		ArchiveDigest:      validated.Digest, ArchiveBytes: validated.ArchiveBytes,
		ContentBytes: validated.ContentBytes, ManifestJSON: string(manifestJSON),
		ConfigurationJSON: "{}", SourceType: source.kind, SourceID: source.id,
		State: model.ThemeStateInstalled, ValidatedAt: now, CreatorID: source.actor,
		CreatedAt: now, UpdatedAt: now,
	}
	return pkg, nil
}
