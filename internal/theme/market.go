package theme

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	maxCatalogBytes        = 2 << 20  // 2 MiB
	maxPackageBytes        = 25 << 20 // 25 MiB
	marketDialTimeout      = 10 * time.Second
	marketHeaderTimeout    = 10 * time.Second
	marketMaxRedirects     = 3
	catalogSignatureHeader = "X-Beat-Catalog-Signature"
)

// MarketClient 以 SSRF 安全的方式获取并校验市场目录与包。
type MarketClient struct {
	client       *http.Client
	allowPrivate bool
	now          func() time.Time
}

// NewMarketClient 构造市场客户端；allowPrivate 仅供测试注入的 loopback 源。
func NewMarketClient(allowPrivate bool, now func() time.Time) *MarketClient {
	if now == nil {
		now = time.Now
	}
	dialer := &net.Dialer{Timeout: marketDialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DialContext: marketDialContext(dialer, allowPrivate),
		TLSHandshakeTimeout: marketDialTimeout, ResponseHeaderTimeout: marketHeaderTimeout,
	}
	return &MarketClient{client: &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= marketMaxRedirects {
				return errors.New("theme: too many redirects")
			}
			return validateMarketURL(request.URL, allowPrivate)
		},
	}, allowPrivate: allowPrivate, now: now}
}

// SetRootCAs 设置自定义根证书池；仅测试注入的 loopback TLS 源使用。
func (client *MarketClient) SetRootCAs(pool *x509.CertPool) {
	if transport, ok := client.client.Transport.(*http.Transport); ok {
		tlsConfig := transport.TLSClientConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		}
		tlsConfig.RootCAs = pool
		transport.TLSClientConfig = tlsConfig
	}
}

func marketDialContext(dialer *net.Dialer, allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("theme: split dial target: %w", err)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("theme: resolve dial target: %w", err)
		}
		for _, resolved := range ips {
			if !allowPrivate && (!resolved.IP.IsGlobalUnicast() || resolved.IP.IsPrivate()) {
				return nil, fmt.Errorf("theme: dial target %s is not public", resolved.IP)
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
}

func validateMarketURL(target *url.URL, allowPrivate bool) error {
	if target == nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" {
		return errors.New("theme: market url must be https without user info or fragment")
	}
	if allowPrivate {
		return nil
	}
	if literal := net.ParseIP(target.Hostname()); literal != nil {
		if !literal.IsGlobalUnicast() || literal.IsPrivate() {
			return errors.New("theme: market host is not public")
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), target.Hostname())
	if err != nil {
		return fmt.Errorf("theme: resolve market host: %w", err)
	}
	for _, resolved := range ips {
		if !resolved.IP.IsGlobalUnicast() || resolved.IP.IsPrivate() {
			return errors.New("theme: market host resolves to a non-public address")
		}
	}
	return nil
}

// FetchCatalog 获取目录，校验 Ed25519 签名并解析。
func (client *MarketClient) FetchCatalog(
	ctx context.Context, source *model.ThemeMarketSource,
) (*model.ThemeCatalog, string, error) {
	parsed, err := url.Parse(source.CatalogURL)
	if err != nil {
		return nil, "", ErrMarketInvalid
	}
	if err := validateMarketURL(parsed, client.allowPrivate); err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.CatalogURL, nil)
	if err != nil {
		return nil, "", ErrMarketUnavailable
	}
	request.Header.Set("Accept", "application/json")
	if source.CacheETag != "" {
		request.Header.Set("If-None-Match", source.CacheETag)
	}
	if source.CacheLastModified != "" {
		request.Header.Set("If-Modified-Since", source.CacheLastModified)
	}
	response, err := client.client.Do(request)
	if err != nil {
		return nil, "", ErrMarketUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotModified {
		return nil, "", nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", ErrMarketUnavailable
	}
	signatureRaw := response.Header.Get(catalogSignatureHeader)
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, "", ErrMarketUnavailable
	}
	if len(body) > maxCatalogBytes {
		return nil, "", ErrMarketInvalid
	}
	if err := verifyCatalogSignature(source.PublicKey, signatureRaw, body); err != nil {
		return nil, "", err
	}
	var catalog model.ThemeCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, "", ErrMarketInvalid
	}
	if catalog.SchemaVersion != 1 || catalog.SourceID != source.ID {
		return nil, "", ErrMarketInvalid
	}
	digest := sha256.Sum256(body)
	return &catalog, hex.EncodeToString(digest[:]), nil
}

// DownloadPackage 下载包并返回原始字节与 SHA-256。
func (client *MarketClient) DownloadPackage(
	ctx context.Context, packageURL string,
) ([]byte, string, error) {
	parsed, err := url.Parse(packageURL)
	if err != nil {
		return nil, "", ErrMarketInvalid
	}
	if err := validateMarketURL(parsed, client.allowPrivate); err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, packageURL, nil)
	if err != nil {
		return nil, "", ErrMarketUnavailable
	}
	response, err := client.client.Do(request)
	if err != nil {
		return nil, "", ErrMarketUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, "", ErrMarketUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxPackageBytes+1))
	if err != nil {
		return nil, "", ErrMarketUnavailable
	}
	if len(raw) > maxPackageBytes {
		return nil, "", ErrMarketInvalid
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}

func verifyCatalogSignature(publicKeyBase64, signatureBase64 string, body []byte) error {
	publicKey, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return ErrMarketInvalid
	}
	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrMarketInvalid
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), body, signature) {
		return ErrMarketInvalid
	}
	return nil
}
