package store

import (
	"context"

	"github.com/beat/backend/internal/model"
)

func (store *ThemeStore) CreateMarketSource(ctx context.Context, source *model.ThemeMarketSource) error {
	return store.mutate(ctx, themeWrite{"theme.market.create", "theme_market", source.ID}, func(mutation themeTransaction) error {
		_, err := mutation.tx.ExecContext(ctx, `INSERT INTO theme_market_sources (
			id,name,catalog_url,enabled,sort_order,public_key,key_fingerprint,cache_etag,
			cache_last_modified,fetched_at,expires_at,catalog_digest,last_status,last_error_category,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, source.ID, source.Name, source.CatalogURL, source.Enabled, source.SortOrder,
			source.PublicKey, source.KeyFingerprint, source.CacheETag, source.CacheLastModified, source.FetchedAt,
			source.ExpiresAt, source.CatalogDigest, source.LastStatus, source.LastErrorCategory, source.CreatedAt, source.UpdatedAt)
		return err
	})
}

func (store *ThemeStore) UpdateMarketSource(ctx context.Context, source *model.ThemeMarketSource) error {
	return store.mutate(ctx, themeWrite{"theme.market.update", "theme_market", source.ID}, func(mutation themeTransaction) error {
		result, err := mutation.tx.ExecContext(ctx, `UPDATE theme_market_sources SET name=?,catalog_url=?,enabled=?,sort_order=?,
			public_key=?,key_fingerprint=?,cache_etag=?,cache_last_modified=?,fetched_at=?,expires_at=?,catalog_digest=?,
			last_status=?,last_error_category=?,updated_at=? WHERE id=?`, source.Name, source.CatalogURL, source.Enabled,
			source.SortOrder, source.PublicKey, source.KeyFingerprint, source.CacheETag, source.CacheLastModified, source.FetchedAt,
			source.ExpiresAt, source.CatalogDigest, source.LastStatus, source.LastErrorCategory, source.UpdatedAt, source.ID)
		return requireThemeRow(result, err, ErrThemeMarketNotFound)
	})
}

// UpdateMarketObservation 只更新仍指向同一可信公钥与 URL 的启用源，避免覆盖并发吊销。
func (store *ThemeStore) UpdateMarketObservation(ctx context.Context, source *model.ThemeMarketSource) error {
	return store.mutate(ctx, themeWrite{"theme.market.refresh", "theme_market", source.ID}, func(mutation themeTransaction) error {
		result, err := mutation.tx.ExecContext(ctx, `UPDATE theme_market_sources SET cache_etag=?,cache_last_modified=?,
			fetched_at=?,expires_at=?,catalog_digest=?,last_status=?,last_error_category=?,updated_at=?
			WHERE id=? AND catalog_url=? AND public_key=? AND enabled=1`, source.CacheETag, source.CacheLastModified,
			source.FetchedAt, source.ExpiresAt, source.CatalogDigest, source.LastStatus, source.LastErrorCategory, source.UpdatedAt,
			source.ID, source.CatalogURL, source.PublicKey)
		return requireThemeRow(result, err, ErrThemeMarketSourceID)
	})
}

func (store *ThemeStore) DeleteMarketSource(ctx context.Context, id string) error {
	return store.mutate(ctx, themeWrite{"theme.market.delete", "theme_market", id}, func(mutation themeTransaction) error {
		result, err := mutation.tx.ExecContext(ctx, "DELETE FROM theme_market_sources WHERE id=?", id)
		return requireThemeRow(result, err, ErrThemeMarketNotFound)
	})
}
