package theme

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// MarketService 管理市场源、目录刷新与市场安装。
type MarketService struct {
	store  *store.ThemeStore
	client *MarketClient
	theme  *Service
	now    func() time.Time
}

// NewMarketService 构造市场服务。
func NewMarketService(
	store *store.ThemeStore, client *MarketClient, theme *Service, now func() time.Time,
) *MarketService {
	if now == nil {
		now = time.Now
	}
	return &MarketService{store: store, client: client, theme: theme, now: now}
}

func (service *MarketService) ListSources(ctx context.Context) ([]model.ThemeMarketSource, error) {
	return service.store.ListMarketSources(ctx)
}

func (service *MarketService) CreateSource(
	ctx context.Context, source *model.ThemeMarketSource,
) (*model.ThemeMarketSource, error) {
	source.ID = uuid.NewString()
	source.CreatedAt = service.now()
	source.UpdatedAt = service.now()
	if err := validateMarketURLInput(source); err != nil {
		return nil, err
	}
	if err := service.store.CreateMarketSource(ctx, source); err != nil {
		return nil, err
	}
	return source, nil
}

func (service *MarketService) UpdateSource(
	ctx context.Context, id string, input model.ThemeMarketSource,
) (*model.ThemeMarketSource, error) {
	existing, err := service.store.GetMarketSource(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, store.ErrThemeMarketNotFound
	}
	if input.CatalogURL != "" && input.CatalogURL != existing.CatalogURL {
		if err := validateMarketURLInput(&input); err != nil {
			return nil, err
		}
		existing.CatalogURL = input.CatalogURL
		existing.CacheETag = ""
		existing.CacheLastModified = ""
	}
	if input.Name != "" {
		existing.Name = input.Name
	}
	if input.PublicKey != "" && input.PublicKey != existing.PublicKey {
		existing.PublicKey = input.PublicKey
		existing.KeyFingerprint = input.KeyFingerprint
		existing.CacheETag = ""
		existing.CacheLastModified = ""
	}
	existing.Enabled = input.Enabled
	existing.SortOrder = input.SortOrder
	existing.UpdatedAt = service.now()
	if err := service.store.UpdateMarketSource(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (service *MarketService) DeleteSource(ctx context.Context, id string) error {
	return service.store.DeleteMarketSource(ctx, id)
}

// RefreshCatalog 拉取并缓存已签名目录（有界缓存）。
func (service *MarketService) RefreshCatalog(ctx context.Context, sourceID string) (*model.ThemeCatalog, error) {
	source, err := service.store.GetMarketSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, store.ErrThemeMarketNotFound
	}
	if !source.Enabled {
		return nil, ErrMarketUnavailable
	}
	now := service.now()
	if source.ExpiresAt != nil && now.Before(*source.ExpiresAt) && source.CatalogDigest != "" {
		return nil, nil // 缓存有效
	}
	catalog, digest, err := service.client.FetchCatalog(ctx, source)
	if err != nil {
		source.LastStatus = "failed"
		source.LastErrorCategory = errorCategory(err)
		source.UpdatedAt = now
		_ = service.store.UpdateMarketSource(ctx, source)
		return nil, err
	}
	if catalog == nil {
		return nil, nil // 304 未修改
	}
	source.CatalogDigest = digest
	source.FetchedAt = &now
	expiry := now.Add(15 * time.Minute)
	source.ExpiresAt = &expiry
	source.LastStatus = "ok"
	source.LastErrorCategory = ""
	source.UpdatedAt = now
	if err := service.store.UpdateMarketSource(ctx, source); err != nil {
		return nil, err
	}
	return catalog, nil
}

// InstallFromMarket 下载、校验摘要并按清单安装市场主题。
func (service *MarketService) InstallFromMarket(
	ctx context.Context, sourceID, themeID, actorID string,
) (*model.ThemePackage, error) {
	source, err := service.store.GetMarketSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, store.ErrThemeMarketNotFound
	}
	if !source.Enabled {
		return nil, ErrMarketUnavailable
	}
	// 安装需要新鲜且经过完整校验的目录，不使用缓存。
	catalog, digest, err := service.client.FetchCatalog(ctx, source)
	if err != nil {
		source.LastStatus = "failed"
		source.LastErrorCategory = errorCategory(err)
		source.UpdatedAt = service.now()
		_ = service.store.UpdateMarketSource(ctx, source)
		return nil, err
	}
	now := service.now()
	source.CatalogDigest = digest
	fetched := now
	source.FetchedAt = &fetched
	expiry := now.Add(15 * time.Minute)
	source.ExpiresAt = &expiry
	source.LastStatus = "ok"
	source.LastErrorCategory = ""
	source.UpdatedAt = now
	if err := service.store.UpdateMarketSource(ctx, source); err != nil {
		return nil, err
	}
	var entry *model.ThemeCatalogEntry
	for index := range catalog.Themes {
		if catalog.Themes[index].ThemeID == themeID {
			entry = &catalog.Themes[index]
			break
		}
	}
	if entry == nil {
		return nil, ErrMarketInvalid
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
	if validated.Manifest.ID != entry.ThemeID || validated.Manifest.Version != entry.Version {
		return nil, ErrMarketInvalid
	}
	return service.theme.Install(ctx, raw, model.ThemeSourceMarket, sourceID, actorID)
}

func validateMarketURLInput(source *model.ThemeMarketSource) error {
	if source.Name == "" || source.CatalogURL == "" || source.PublicKey == "" {
		return fmt.Errorf("theme: market source requires name, catalog url and public key")
	}
	return nil
}

func errorCategory(err error) string {
	switch {
	case errors.Is(err, ErrMarketInvalid):
		return "invalid_catalog"
	case errors.Is(err, ErrMarketUnavailable):
		return "unavailable"
	default:
		return "unknown"
	}
}
