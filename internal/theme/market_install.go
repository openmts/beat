package theme

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

var ErrMarketChanged = errors.New("theme: selected catalog entry changed")

// MarketSelection 绑定操作者实际看到并选择的版本与归档，禁止静默安装新版本。
type MarketSelection struct {
	SourceID      string
	ThemeID       string
	Version       string
	PackageSHA256 string
}

// InstallFromMarket 绑定已验签目录、原始 manifest 与归档；只安装，不激活。
func (service *MarketService) InstallFromMarket(ctx context.Context, selection MarketSelection, actorID string) (*model.ThemePackage, error) {
	if !validThemeID(selection.ThemeID) || !validSemver(selection.Version) || !validDigest(selection.PackageSHA256) {
		return nil, ErrMarketInvalid
	}
	source, err := service.enabledSource(ctx, selection.SourceID)
	if err != nil {
		return nil, err
	}
	catalog, err := service.catalog(ctx, source, false)
	if err != nil {
		return nil, err
	}
	entry, err := selectedCatalogTheme(catalog, selection)
	if err != nil {
		return nil, err
	}
	raw, digest, err := service.client.DownloadPackage(ctx, entry.PackageURL)
	if err != nil {
		return nil, err
	}
	if digest != entry.PackageSHA256 {
		return nil, ErrMarketInvalid
	}
	validated, err := ValidateArchive(raw)
	if err != nil {
		return nil, err
	}
	if err := bindCatalogPackage(entry, validated); err != nil {
		return nil, err
	}
	if !service.now().Before(catalog.CacheExpiresAt) || !service.now().Before(catalog.ExpiresAt) {
		return nil, errors.Join(ErrMarketChanged, ErrMarketExpired)
	}
	ctx = store.WithThemeMarketBinding(ctx, store.ThemeMarketBinding{SourceID: source.ID, CatalogURL: source.CatalogURL,
		PublicKey: source.PublicKey, CatalogDigest: catalog.Digest, ExpiresAt: catalog.CacheExpiresAt, Now: service.now})
	return service.theme.Install(ctx, raw, model.ThemeSourceMarket, selection.SourceID, actorID)
}

func selectedCatalogTheme(catalog *CatalogResult, selection MarketSelection) (model.ThemeCatalogEntry, error) {
	for _, entry := range catalog.Themes {
		if entry.ThemeID == selection.ThemeID {
			if entry.Version != selection.Version || entry.PackageSHA256 != selection.PackageSHA256 {
				return model.ThemeCatalogEntry{}, ErrMarketChanged
			}
			return entry, nil
		}
	}
	return model.ThemeCatalogEntry{}, ErrMarketChanged
}

func bindCatalogPackage(entry model.ThemeCatalogEntry, validated *ValidatedPackage) error {
	manifest := validated.Manifest
	if sha256Hex(validated.ManifestBytes) != entry.ManifestSHA256 || validated.Digest != entry.PackageSHA256 ||
		manifest.ID != entry.ThemeID || manifest.Version != entry.Version || manifest.Runtime != entry.Runtime ||
		manifest.MinimumBeatVersion != entry.MinimumBeat || manifest.MaximumBeatVersion != entry.MaximumBeat {
		return ErrMarketInvalid
	}
	return nil
}

func (service *MarketService) enabledSource(ctx context.Context, id string) (*model.ThemeMarketSource, error) {
	source, err := service.store.GetMarketSource(ctx, id)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, store.ErrThemeMarketNotFound
	}
	if !source.Enabled {
		return nil, ErrMarketUnavailable
	}
	return source, nil
}

func (service *MarketService) catalog(ctx context.Context, source *model.ThemeMarketSource, force bool) (*CatalogResult, error) {
	result, err := service.client.fetchCatalog(ctx, source, force)
	source.UpdatedAt = service.now()
	if err != nil {
		clearMarketCache(source)
		source.LastStatus, source.LastErrorCategory = "failed", errorCategory(err)
		return nil, errors.Join(err, service.store.UpdateMarketObservation(ctx, source))
	}
	source.FetchedAt, source.ExpiresAt = &result.FetchedAt, &result.CacheExpiresAt
	source.CacheETag, source.CacheLastModified, source.CatalogDigest = result.ETag, result.LastModified, result.Digest
	source.LastStatus, source.LastErrorCategory = "ok", ""
	if result.Stale {
		source.LastStatus, source.LastErrorCategory = "stale", result.ErrorCategory
	}
	if err := service.store.UpdateMarketObservation(ctx, source); err != nil {
		return nil, err
	}
	return result, nil
}

func errorCategory(err error) string {
	switch {
	case errors.Is(err, ErrMarketExpired):
		return "expired"
	case errors.Is(err, ErrMarketInvalid):
		return "invalid_catalog"
	case errors.Is(err, ErrMarketUnavailable):
		return "unavailable"
	default:
		return "unknown"
	}
}
