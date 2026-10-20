package agentupdate

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const transferRequestTimeout = 30 * time.Second

const artifactPathPrefix = "/api/v1/agents/rollout/artifacts"

type HTTPConfig struct {
	ServerURL  string
	AgentToken string
	PathPrefix string
	TLS        *tls.Config
}

type HTTPFetcher struct {
	server        *url.URL
	prefix, token string
	client        *http.Client
}

// NewHTTPFetcher 的origin/prefix/token只来自当前可信Agent配置，不接受manifest URL。
func NewHTTPFetcher(config HTTPConfig) (*HTTPFetcher, error) {
	server, err := trustedServer(config.ServerURL)
	if err != nil {
		return nil, err
	}
	if config.AgentToken == "" || strings.ContainsAny(config.AgentToken, "\r\n\x00") {
		return nil, ErrConfiguration
	}
	if config.PathPrefix == "" {
		config.PathPrefix = artifactPathPrefix
	}
	if config.PathPrefix != artifactPathPrefix {
		return nil, ErrConfiguration
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if config.TLS != nil {
		tlsConfig = config.TLS.Clone()
	}
	if tlsConfig.InsecureSkipVerify || (tlsConfig.ServerName != "" && tlsConfig.ServerName != server.Hostname()) {
		return nil, ErrConfiguration
	}
	tlsConfig.MinVersion = max(tlsConfig.MinVersion, tls.VersionTLS12)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{
		Proxy: nil, TLSClientConfig: tlsConfig, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10,
		DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
	}}
	return &HTTPFetcher{server: server, prefix: config.PathPrefix, token: config.AgentToken, client: client}, nil
}

func (fetcher *HTTPFetcher) Fetch(ctx context.Context, input FetchRequest) (FetchResponse, error) {
	if !validFetchRequest(input) {
		return FetchResponse{}, ErrAssignment
	}
	endpoint := *fetcher.server
	endpoint.Path += fetcher.prefix + "/" + url.PathEscape(input.Assignment.ID) + "/" + url.PathEscape(input.Assignment.Artifact.ID)
	bounded, cancel := context.WithTimeout(ctx, transferRequestTimeout)
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		cancel()
		return FetchResponse{}, ErrDownload
	}
	request.Header.Set("Authorization", "Bearer "+fetcher.token)
	request.Header.Set("X-Beat-Rollout-Nonce", input.Assignment.Nonce)
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", input.Offset, input.Offset+input.Length-1))
	request.Header.Set("Accept-Encoding", "identity")
	response, err := fetcher.client.Do(request)
	if err != nil {
		cancel()
		return FetchResponse{}, ErrDownload
	}
	if !validRangeResponse(response, input) {
		_ = response.Body.Close()
		cancel()
		return FetchResponse{}, ErrDownload
	}
	return FetchResponse{Body: &transferBody{ReadCloser: response.Body, cancel: cancel}, Offset: input.Offset, Length: input.Length, Total: input.Assignment.Artifact.Size}, nil
}

func validFetchRequest(input FetchRequest) bool {
	artifact := input.Assignment.Artifact
	return protocol.ValidSigningKeyID(input.Assignment.ID) && protocol.ValidSigningKeyID(artifact.ID) && protocol.ValidSHA256(input.Assignment.Nonce) && artifact.Size > 0 && artifact.Size <= protocol.MaximumArtifactBytes && input.Offset >= 0 && input.Length > 0 && input.Length <= ChunkBytes && input.Offset <= artifact.Size-input.Length
}

func validRangeResponse(response *http.Response, input FetchRequest) bool {
	if response.ContentLength != input.Length {
		return false
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return false
	}
	if response.StatusCode == http.StatusOK {
		return input.Offset == 0 && input.Length == input.Assignment.Artifact.Size
	}
	if response.StatusCode != http.StatusPartialContent {
		return false
	}
	expected := fmt.Sprintf("bytes %d-%d/%d", input.Offset, input.Offset+input.Length-1, input.Assignment.Artifact.Size)
	return response.Header.Get("Content-Range") == expected
}

type transferBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body *transferBody) Close() error { body.cancel(); return body.ReadCloser.Close() }

func (fetcher *HTTPFetcher) Close() { fetcher.client.CloseIdleConnections() }

func (fetcher *HTTPFetcher) TrustedServer() string { return fetcher.server.String() }
