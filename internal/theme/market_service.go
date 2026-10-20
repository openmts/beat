package theme

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type MarketService struct {
	store      *store.ThemeStore
	client     *MarketClient
	theme      *Service
	now        func() time.Time
	sourceGate operationGate
}

func NewMarketService(store *store.ThemeStore, client *MarketClient, theme *Service, now func() time.Time) *MarketService {
	if now == nil {
		now = time.Now
	}
	return &MarketService{store: store, client: client, theme: theme, now: now}
}

func (service *MarketService) ListSources(ctx context.Context) ([]model.ThemeMarketSource, error) {
	return service.store.ListMarketSources(ctx)
}

func (service *MarketService) CreateSource(ctx context.Context, input *model.ThemeMarketSource) (*model.ThemeMarketSource, error) {
	if input == nil {
		return nil, ErrMarketInvalid
	}
	source := *input
	if err := service.validateSource(&source); err != nil {
		return nil, err
	}
	source.ID, source.CreatedAt, source.UpdatedAt = uuid.NewString(), service.now(), service.now()
	clearMarketCache(&source)
	if err := service.store.CreateMarketSource(ctx, &source); err != nil {
		return nil, err
	}
	return &source, nil
}

func (service *MarketService) UpdateSource(ctx context.Context, id string, input model.ThemeMarketSource) (*model.ThemeMarketSource, error) {
	release, err := service.sourceGate.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	existing, err := service.store.GetMarketSource(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, store.ErrThemeMarketNotFound
	}
	next := mergeMarketSource(*existing, input)
	if err := service.validateSource(&next); err != nil {
		return nil, err
	}
	if err := service.client.invalidateSource(ctx, id); err != nil {
		return nil, err
	}
	if existing.PublicKey != next.PublicKey || existing.CatalogURL != next.CatalogURL || existing.Enabled != next.Enabled {
		clearMarketCache(&next)
	}
	next.UpdatedAt = service.now()
	if err := service.store.UpdateMarketSource(ctx, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

func (service *MarketService) DeleteSource(ctx context.Context, id string) error {
	release, err := service.sourceGate.enter(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := service.client.invalidateSource(ctx, id); err != nil {
		return err
	}
	return service.store.DeleteMarketSource(ctx, id)
}

// RefreshCatalog 强制联系源；304 与有界回退均返回完整已验签目录。
func (service *MarketService) RefreshCatalog(ctx context.Context, sourceID string) (*CatalogResult, error) {
	source, err := service.enabledSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return service.catalog(ctx, source, true)
}

func mergeMarketSource(existing, input model.ThemeMarketSource) model.ThemeMarketSource {
	if input.Name != "" {
		existing.Name = input.Name
	}
	if input.CatalogURL != "" {
		existing.CatalogURL = input.CatalogURL
	}
	if input.PublicKey != "" {
		existing.PublicKey = input.PublicKey
	}
	existing.KeyFingerprint = input.KeyFingerprint
	existing.Enabled, existing.SortOrder = input.Enabled, input.SortOrder
	return existing
}

func (service *MarketService) validateSource(source *model.ThemeMarketSource) error {
	source.Name, source.CatalogURL = strings.TrimSpace(source.Name), strings.TrimSpace(source.CatalogURL)
	source.PublicKey = strings.TrimSpace(source.PublicKey)
	identity, err := MarketIdentity(source.PublicKey)
	if err != nil {
		return err
	}
	if source.Name == "" || len(source.Name) > 200 || len(source.CatalogURL) > 2048 ||
		source.KeyFingerprint != "" && source.KeyFingerprint != identity {
		return ErrMarketInvalid
	}
	target, err := url.Parse(source.CatalogURL)
	if err != nil {
		return ErrMarketInvalid
	}
	if err := validateMarketURL(target, service.client.allowPrivate); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(source.PublicKey)
	if err != nil {
		return ErrMarketInvalid
	}
	source.PublicKey, source.KeyFingerprint = base64.StdEncoding.EncodeToString(raw), identity
	return nil
}

func clearMarketCache(source *model.ThemeMarketSource) {
	source.CacheETag, source.CacheLastModified, source.CatalogDigest = "", "", ""
	source.FetchedAt, source.ExpiresAt = nil, nil
	source.LastStatus, source.LastErrorCategory = "", ""
}
