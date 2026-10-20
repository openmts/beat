package theme

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/beat/backend/internal/model"
)

func (client *MarketClient) requestCatalog(ctx context.Context, source *model.ThemeMarketSource, cached *catalogCacheEntry) (*catalogCacheEntry, *model.ThemeCatalog, bool, error) {
	target, err := url.Parse(source.CatalogURL)
	if err != nil {
		return nil, nil, false, ErrMarketInvalid
	}
	if err := validateMarketURL(target, client.allowPrivate); err != nil {
		return nil, nil, false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, false, ErrMarketInvalid
	}
	request.Header.Set("Accept", "application/json")
	if cached != nil {
		if cached.ETag != "" {
			request.Header.Set("If-None-Match", cached.ETag)
		}
		if cached.LastModified != "" {
			request.Header.Set("If-Modified-Since", cached.LastModified)
		}
	}
	response, err := client.client.Do(request)
	if err != nil {
		return nil, nil, false, marketTransportError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotModified {
		if cached == nil {
			return nil, nil, false, ErrMarketInvalid
		}
		catalog, err := verifyMarketCatalog(cached.Body, cached.Signature, catalogVerification{publicKey: source.PublicKey, now: client.now(), allowPrivate: client.allowPrivate})
		if err != nil {
			return nil, nil, false, err
		}
		copy := *cached
		updateCatalogFreshness(&copy, catalog, client.now())
		if etag := validMarketETag(response.Header.Get("ETag")); etag != "" {
			copy.ETag = etag
		}
		return &copy, catalog, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, false, ErrMarketUnavailable
	}
	return client.readCatalogResponse(response, source)
}

func (client *MarketClient) readCatalogResponse(response *http.Response, source *model.ThemeMarketSource) (*catalogCacheEntry, *model.ThemeCatalog, bool, error) {
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, nil, false, ErrMarketUnavailable
	}
	signature := response.Header.Get(catalogSignatureHeader)
	catalog, err := verifyMarketCatalog(body, signature, catalogVerification{publicKey: source.PublicKey, now: client.now(), allowPrivate: client.allowPrivate})
	if err != nil {
		return nil, nil, false, err
	}
	entry := &catalogCacheEntry{Body: body, Signature: signature, Digest: sha256Hex(body), SourceID: source.ID, ETag: validMarketETag(response.Header.Get("ETag"))}
	if modified, err := http.ParseTime(response.Header.Get("Last-Modified")); err == nil {
		entry.LastModified = modified.UTC().Format(http.TimeFormat)
	}
	updateCatalogFreshness(entry, catalog, client.now())
	return entry, catalog, false, nil
}

func updateCatalogFreshness(entry *catalogCacheEntry, catalog *model.ThemeCatalog, now time.Time) {
	entry.FetchedAt, entry.ExpiresAt, entry.Failure = now, now.Add(catalogCacheLifetime), ""
	if catalog.ExpiresAt.Before(entry.ExpiresAt) {
		entry.ExpiresAt = catalog.ExpiresAt
	}
}

func validMarketETag(value string) string {
	if len(value) == 0 || len(value) > 1024 {
		return ""
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return ""
		}
	}
	return value
}

func marketTransportError(err error) error {
	if errors.Is(err, ErrMarketInvalid) {
		return ErrMarketInvalid
	}
	return ErrMarketUnavailable
}
