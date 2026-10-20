package theme

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

const maximumCatalogLifetime = 7 * 24 * time.Hour

const maximumCatalogEntries = 200

var ErrMarketExpired = fmt.Errorf("%w: catalog expired", ErrMarketInvalid)

type catalogVerification struct {
	publicKey    string
	now          time.Time
	allowPrivate bool
}

// MarketIdentity 是发布者签名公钥的稳定公共身份，独立于各实例的源 UUID。
func MarketIdentity(publicKey string) (string, error) {
	raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(publicKey))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return "", ErrMarketInvalid
	}
	return "sha256:" + sha256Hex(raw), nil
}

func verifyMarketCatalog(body []byte, signature string, options catalogVerification) (*model.ThemeCatalog, error) {
	if len(body) == 0 || len(body) > maxCatalogBytes || !utf8.Valid(body) {
		return nil, ErrMarketInvalid
	}
	if err := verifyCatalogSignature(strings.TrimSpace(options.publicKey), signature, body); err != nil {
		return nil, err
	}
	var catalog model.ThemeCatalog
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return nil, ErrMarketInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, ErrMarketInvalid
	}
	identity, err := MarketIdentity(options.publicKey)
	if err != nil || catalog.SchemaVersion != 1 || catalog.SourceID != identity || catalog.ExpiresAt.IsZero() {
		return nil, ErrMarketInvalid
	}
	if !catalog.ExpiresAt.After(options.now) {
		return nil, ErrMarketExpired
	}
	if catalog.ExpiresAt.After(options.now.Add(maximumCatalogLifetime)) || len(catalog.Themes) > maximumCatalogEntries {
		return nil, ErrMarketInvalid
	}
	seen := make(map[string]bool, len(catalog.Themes))
	for index := range catalog.Themes {
		entry := catalog.Themes[index]
		if seen[entry.ThemeID] {
			return nil, ErrMarketInvalid
		}
		if err := validateCatalogEntry(entry, options.allowPrivate); err != nil {
			return nil, err
		}
		// 目录里的声明与清单同一形状：校验通过后再规范化（补 type、按键收窄选项），
		// 让后台拿到与预览描述符完全一致的 schema。
		catalog.Themes[index].Configuration = describeConfigurationSchema(entry.Configuration)
		seen[entry.ThemeID] = true
	}
	return &catalog, nil
}

func validateCatalogEntry(entry model.ThemeCatalogEntry, allowPrivate bool) error {
	if !validThemeID(entry.ThemeID) || !validSemver(entry.Version) || !validSemver(entry.MinimumBeat) || entry.MaximumBeat != "" && !validSemver(entry.MaximumBeat) {
		return ErrMarketInvalid
	}
	if strings.TrimSpace(entry.Name) == "" || len(entry.Name) > 200 || len(entry.Author) > 200 || len(entry.Description) > 2048 || len(entry.ReleaseNotes) > 4096 {
		return ErrMarketInvalid
	}
	if entry.Runtime != model.ThemeRuntimeDeclarative && entry.Runtime != model.ThemeRuntimeSandboxed {
		return ErrMarketInvalid
	}
	if !validMarketDigest(entry.PackageSHA256) || !validMarketDigest(entry.ManifestSHA256) {
		return ErrMarketInvalid
	}
	if entry.MaximumBeat != "" && compareVersions(parseVersion(entry.MinimumBeat), parseVersion(entry.MaximumBeat)) > 0 {
		return ErrMarketInvalid
	}
	target, err := url.Parse(entry.PackageURL)
	if err != nil || len(entry.PackageURL) > 2048 {
		return ErrMarketInvalid
	}
	if err := validateManifestConfiguration(entry.Configuration); err != nil {
		return ErrMarketInvalid
	}
	return validateMarketURL(target, allowPrivate)
}

func validMarketDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
