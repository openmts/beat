package theme

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
	marketRequestTimeout   = 30 * time.Second
	catalogSignatureHeader = "X-Beat-Catalog-Signature"
)

// MarketClient 以 SSRF 安全的方式获取并校验市场目录与包。
type MarketClient struct {
	client          *http.Client
	allowPrivate    bool
	now             func() time.Time
	catalogGate     operationGate
	catalogs        map[string]*catalogCacheEntry
	catalogSequence uint64
}

// NewMarketClient 构造市场客户端；allowPrivate 放通私网来源，由运维开关显式开启，
// 测试注入的 loopback 源同样复用该开关。
func NewMarketClient(allowPrivate bool, now func() time.Time) *MarketClient {
	if now == nil {
		now = time.Now
	}
	dialer := &net.Dialer{Timeout: marketDialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DialContext: marketDialContext(dialer, allowPrivate),
		TLSHandshakeTimeout: marketDialTimeout, ResponseHeaderTimeout: marketHeaderTimeout,
		DisableKeepAlives: true, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &MarketClient{client: &http.Client{
		Transport: transport, Timeout: marketRequestTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > marketMaxRedirects {
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

// FetchCatalog 获取目录，校验 Ed25519 签名并解析。
func (client *MarketClient) FetchCatalog(ctx context.Context, source *model.ThemeMarketSource) (*model.ThemeCatalog, string, error) {
	result, err := client.fetchCatalog(ctx, source, false)
	if err != nil {
		return nil, "", err
	}
	return result.ThemeCatalog, result.Digest, nil
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
		return nil, "", marketTransportError(err)
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
