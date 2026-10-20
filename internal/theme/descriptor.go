package theme

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (service *Service) DescribePackage(ctx context.Context, id string) (model.ActiveThemeDescriptor, error) {
	pkg, err := service.store.GetPackageByID(ctx, id)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	if pkg == nil || pkg.State != model.ThemeStateInstalled {
		return model.ActiveThemeDescriptor{}, ErrContentUnavailable
	}
	return service.describeStoredPackage(ctx, pkg)
}

func (service *Service) describeStoredPackage(ctx context.Context, pkg *model.ThemePackage) (model.ActiveThemeDescriptor, error) {
	manifest, err := parseManifest([]byte(pkg.ManifestJSON))
	if err != nil {
		return model.ActiveThemeDescriptor{}, ErrContentUnavailable
	}
	descriptor := model.ActiveThemeDescriptor{PackageID: pkg.ID, ThemeID: pkg.ThemeID, Name: pkg.Name, Version: pkg.Version,
		Runtime: pkg.Runtime, Protocol: 2, Configuration: pkg.ConfigurationJSON, ArchiveDigest: pkg.ArchiveDigest,
		PreviewURL: themeAssetPath(pkg.ArchiveDigest, manifest.Preview)}
	if pkg.Runtime == model.ThemeRuntimeSandboxed {
		descriptor.SandboxEntry, err = sandboxEntry(manifest)
		if err != nil {
			return model.ActiveThemeDescriptor{}, err
		}
		if _, err := service.ReadAsset(ctx, pkg.ArchiveDigest, descriptor.SandboxEntry); err != nil {
			return model.ActiveThemeDescriptor{}, err
		}
		return descriptor, nil
	}
	asset, err := service.ReadAsset(ctx, pkg.ArchiveDigest, definitionName)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	definition, err := ParseDefinition(asset.Content)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	configuration, err := ParseThemeConfiguration([]byte(pkg.ConfigurationJSON), manifest.Configuration)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	resolved := ApplyThemeConfiguration(definition, configuration)
	descriptor.ConfigurationSchema = describeConfigurationSchema(manifest.Configuration)
	descriptor.Declarative = &resolved
	return descriptor, nil
}

func (service *Service) Configure(ctx context.Context, id string, raw []byte) (model.ActiveThemeDescriptor, error) {
	release, err := service.mutations.enter(ctx)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	defer release()
	current, err := service.DescribePackage(ctx, id)
	if err != nil {
		return current, err
	}
	if current.Runtime != model.ThemeRuntimeDeclarative {
		return model.ActiveThemeDescriptor{}, ErrRuntimeIncompatible
	}
	configuration, err := ParseThemeConfiguration(raw, current.ConfigurationSchema)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		return model.ActiveThemeDescriptor{}, ErrManifestInvalid
	}
	if err := service.store.UpdateConfiguration(ctx, store.ThemeConfigurationUpdate{PackageID: id, JSON: string(encoded), At: service.now()}); err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	return service.DescribePackage(ctx, id)
}

func themeAssetPath(digest, path string) string {
	parts := strings.Split(path, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return "/themes/assets/" + digest + "/" + strings.Join(parts, "/")
}
