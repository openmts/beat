package theme

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

const maximumCachedCatalogs = 16

const catalogCacheLifetime = 15 * time.Minute

// CatalogResult 的缓存状态仅属于服务端响应，不参与上游签名载荷。
type CatalogResult struct {
	*model.ThemeCatalog
	Digest         string    `json:"catalog_digest"`
	Cached         bool      `json:"cached"`
	Stale          bool      `json:"stale"`
	ErrorCategory  string    `json:"error_category,omitempty"`
	FetchedAt      time.Time `json:"fetched_at"`
	CacheExpiresAt time.Time `json:"cache_expires_at"`
	ETag           string    `json:"-"`
	LastModified   string    `json:"-"`
}

type catalogCacheEntry struct {
	Body         []byte
	Signature    string
	Digest       string
	ETag         string
	LastModified string
	FetchedAt    time.Time
	ExpiresAt    time.Time
	Failure      string
	Sequence     uint64
	SourceID     string
}

func (entry *catalogCacheEntry) result(catalog *model.ThemeCatalog, cached bool) *CatalogResult {
	return &CatalogResult{ThemeCatalog: catalog, Digest: entry.Digest, Cached: cached, Stale: entry.Failure != "", ErrorCategory: entry.Failure,
		FetchedAt: entry.FetchedAt, CacheExpiresAt: entry.ExpiresAt, ETag: entry.ETag, LastModified: entry.LastModified}
}

func (client *MarketClient) fetchCatalog(ctx context.Context, source *model.ThemeMarketSource, force bool) (*CatalogResult, error) {
	if source == nil {
		return nil, ErrMarketInvalid
	}
	identity, err := MarketIdentity(source.PublicKey)
	if err != nil {
		return nil, err
	}
	release, err := client.catalogGate.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if client.catalogs == nil {
		client.catalogs = make(map[string]*catalogCacheEntry)
	}
	key := source.ID + "\x00" + source.CatalogURL + "\x00" + identity
	entry := client.catalogs[key]
	var catalog *model.ThemeCatalog
	if entry != nil {
		catalog, err = verifyMarketCatalog(entry.Body, entry.Signature, catalogVerification{publicKey: source.PublicKey, now: client.now(), allowPrivate: client.allowPrivate})
		if err != nil {
			delete(client.catalogs, key)
			entry = nil
		}
	}
	if entry != nil && !force && entry.ExpiresAt.After(client.now()) {
		return entry.result(catalog, true), nil
	}
	next, catalog, cached, err := client.requestCatalog(ctx, source, entry)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, ErrMarketUnavailable) && entry != nil && entry.ExpiresAt.After(client.now()) {
			entry.Failure = "unavailable"
			verified, verifyErr := verifyMarketCatalog(entry.Body, entry.Signature, catalogVerification{publicKey: source.PublicKey, now: client.now(), allowPrivate: client.allowPrivate})
			if verifyErr == nil {
				return entry.result(verified, true), nil
			}
		}
		delete(client.catalogs, key)
		return nil, err
	}
	client.storeCatalog(key, next)
	return next.result(catalog, cached), nil
}

func (client *MarketClient) storeCatalog(key string, entry *catalogCacheEntry) {
	client.catalogSequence++
	entry.Sequence = client.catalogSequence
	if _, exists := client.catalogs[key]; !exists && len(client.catalogs) >= maximumCachedCatalogs {
		oldest := ""
		for candidate, value := range client.catalogs {
			if oldest == "" || value.Sequence < client.catalogs[oldest].Sequence {
				oldest = candidate
			}
		}
		delete(client.catalogs, oldest)
	}
	client.catalogs[key] = entry
}

func (client *MarketClient) invalidateSource(ctx context.Context, id string) error {
	release, err := client.catalogGate.enter(ctx)
	if err != nil {
		return err
	}
	defer release()
	for key, entry := range client.catalogs {
		if entry.SourceID == id {
			delete(client.catalogs, key)
		}
	}
	return nil
}
