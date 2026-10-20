package theme

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"sort"
	"time"

	"github.com/beat/backend/internal/model"
)

type MarketPackageInput struct {
	Archive []byte
	URL     string
}

type MarketPublishOptions struct {
	PrivateKey ed25519.PrivateKey
	Packages   []MarketPackageInput
	ExpiresAt  time.Time
	Now        time.Time
}

type PublishedMarketCatalog struct {
	Body      []byte
	Signature string
	SourceID  string
}

// BuildMarketCatalog 使用生产归档校验器生成两个原始摘要和公钥公共身份。
func BuildMarketCatalog(options MarketPublishOptions) (PublishedMarketCatalog, error) {
	if len(options.PrivateKey) != ed25519.PrivateKeySize || len(options.Packages) > maximumCatalogEntries {
		return PublishedMarketCatalog{}, ErrMarketInvalid
	}
	canonical := ed25519.NewKeyFromSeed(options.PrivateKey[:ed25519.SeedSize])
	if !bytes.Equal(canonical, options.PrivateKey) {
		return PublishedMarketCatalog{}, ErrMarketInvalid
	}
	public := base64.StdEncoding.EncodeToString(options.PrivateKey[ed25519.SeedSize:])
	identity, err := MarketIdentity(public)
	if err != nil {
		return PublishedMarketCatalog{}, err
	}
	entries, err := publishMarketEntries(options.Packages)
	if err != nil {
		return PublishedMarketCatalog{}, err
	}
	catalog := model.ThemeCatalog{SchemaVersion: 1, SourceID: identity, ExpiresAt: options.ExpiresAt, Themes: entries}
	body, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return PublishedMarketCatalog{}, err
	}
	body = append(body, '\n')
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(options.PrivateKey, body))
	if _, err := verifyMarketCatalog(body, signature, catalogVerification{publicKey: public, now: options.Now}); err != nil {
		return PublishedMarketCatalog{}, err
	}
	return PublishedMarketCatalog{Body: body, Signature: signature, SourceID: identity}, nil
}

func publishMarketEntries(inputs []MarketPackageInput) ([]model.ThemeCatalogEntry, error) {
	entries := make([]model.ThemeCatalogEntry, 0, len(inputs))
	for _, input := range inputs {
		validated, err := ValidateArchive(input.Archive)
		if err != nil {
			return nil, err
		}
		manifest := validated.Manifest
		entries = append(entries, model.ThemeCatalogEntry{ThemeID: manifest.ID, Name: manifest.Name, Version: manifest.Version,
			Author: manifest.Author, Description: manifest.Description, Homepage: manifest.Homepage, Runtime: manifest.Runtime,
			PackageURL: input.URL, PackageSHA256: validated.Digest, ManifestSHA256: sha256Hex(validated.ManifestBytes),
			MinimumBeat: manifest.MinimumBeatVersion, MaximumBeat: manifest.MaximumBeatVersion, ReleaseNotes: manifest.ReleaseNotes,
			// 站点可配置项随目录一起签名发布，后台在安装前就能看到这个包会放开哪些开关。
			Configuration: manifest.Configuration})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].ThemeID < entries[right].ThemeID })
	return entries, nil
}
