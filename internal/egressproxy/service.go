package egressproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

var (
	ErrInvalid      = errors.New("invalid egress proxy settings")
	ErrConflict     = errors.New("egress proxy settings changed")
	ErrUnavailable  = errors.New("egress proxy unavailable")
	ErrDisabled     = errors.New("egress proxy is disabled")
	ErrProbeFailed  = errors.New("egress proxy probe failed")
	ErrProbeTimeout = errors.New("egress proxy probe timed out")
)

const (
	probeTarget        = "https://example.com/"
	probeMarker        = "Example Domain"
	probeTimeout       = 12 * time.Second
	probeHeaderTimeout = 8 * time.Second
	probeBodyLimit     = 64 * 1024
)

type Settings struct {
	Enabled     bool      `json:"enabled"`
	ProxyURL    string    `json:"proxy_url"`
	Username    string    `json:"username"`
	HasPassword bool      `json:"has_password"`
	Revision    int64     `json:"revision"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Update struct {
	Enabled          bool    `json:"enabled"`
	ProxyURL         string  `json:"proxy_url"`
	Username         string  `json:"username"`
	Password         *string `json:"password,omitempty"`
	ClearPassword    bool    `json:"clear_password"`
	ExpectedRevision int64   `json:"expected_revision"`
}

type ProbeResult struct {
	CheckedAt time.Time `json:"checked_at"`
	LatencyMS int64     `json:"latency_ms"`
}

type secretPayload struct {
	ProxyURL string `json:"proxy_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type loadedSettings struct {
	Settings
	secret secretPayload
	set    bool
}

type Service struct {
	store       *store.EgressProxyStore
	secrets     secretbox.ResourceService
	probeClient *http.Client
}

func NewService(store *store.EgressProxyStore, secrets secretbox.ResourceService) (*Service, error) {
	if store == nil || secrets == nil {
		return nil, ErrUnavailable
	}
	return &Service{store: store, secrets: secrets}, nil
}

func (service *Service) Get(ctx context.Context) (Settings, error) {
	loaded, err := service.load(ctx)
	return loaded.Settings, err
}

func (service *Service) load(ctx context.Context) (loadedSettings, error) {
	record, err := service.store.Get(ctx)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return loadedSettings{}, err
	}
	if err != nil {
		return loadedSettings{}, ErrUnavailable
	}
	if len(record.Encrypted) == 0 {
		if record.Enabled {
			return loadedSettings{}, ErrUnavailable
		}
		return loadedSettings{Settings: settingsFromRecord(record, secretPayload{}, false)}, nil
	}
	plain, err := service.secrets.OpenResource(ctx, record.Encrypted, secretbox.EgressProxyAAD(record.Revision))
	if err != nil {
		return loadedSettings{}, ErrUnavailable
	}
	defer clear(plain)
	var secret secretPayload
	if err := json.Unmarshal(plain, &secret); err != nil {
		return loadedSettings{}, ErrUnavailable
	}
	return loadedSettings{Settings: settingsFromRecord(record, secret, true), secret: secret, set: true}, nil
}

func settingsFromRecord(record store.EgressProxyRecord, secret secretPayload, set bool) Settings {
	view := Settings{Enabled: record.Enabled, Revision: record.Revision, UpdatedAt: record.UpdatedAt}
	if set {
		view.ProxyURL = secret.ProxyURL
		view.Username = secret.Username
		view.HasPassword = secret.Password != ""
	}
	return view
}

func (service *Service) Update(ctx context.Context, input Update) (Settings, error) {
	if input.ExpectedRevision <= 0 {
		return Settings{}, ErrInvalid
	}
	current, err := service.load(ctx)
	if err != nil {
		return Settings{}, err
	}
	normalized, err := normalizeInput(input, current.secret, current.set)
	if err != nil {
		return Settings{}, err
	}
	plain, err := json.Marshal(normalized)
	if err != nil {
		return Settings{}, ErrUnavailable
	}
	defer clear(plain)
	revision := current.Revision + 1
	var committed store.EgressProxyRecord
	err = service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		encrypted, sealErr := access.Seal(plain, secretbox.EgressProxyAAD(revision))
		if sealErr != nil {
			return ErrUnavailable
		}
		var updateErr error
		committed, updateErr = service.store.UpdateEncrypted(ctx, input.Enabled, encrypted, input.ExpectedRevision)
		if errors.Is(updateErr, store.ErrEgressProxyConflict) {
			return ErrConflict
		}
		if updateErr != nil {
			return updateErr
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	// 配置已在事务中提交；不要用可能已取消的请求上下文重新解密读取，
	// 否则调用方会收到失败但配置实际已经生效。
	return settingsFromRecord(committed, normalized, true), nil
}

func (service *Service) Proxy(request *http.Request) (*url.URL, error) {
	if request == nil {
		return nil, ErrInvalid
	}
	settings, err := service.load(request.Context())
	if err != nil {
		return nil, err
	}
	if !settings.Enabled || settings.ProxyURL == "" {
		return nil, nil
	}
	proxyURL, err := url.Parse(settings.ProxyURL)
	if err != nil {
		return nil, ErrUnavailable
	}
	if settings.Username != "" {
		proxyURL.User = url.UserPassword(settings.Username, settings.secret.Password)
	}
	return proxyURL, nil
}

// IsProxyAddress reports whether transport dialing should connect directly to
// the owner-configured proxy host instead of applying the destination's
// public-address policy to it.
func (service *Service) IsProxyAddress(ctx context.Context, address string) (bool, error) {
	loaded, err := service.load(ctx)
	if err != nil {
		return false, err
	}
	if !loaded.Enabled || loaded.ProxyURL == "" {
		return false, nil
	}
	proxyURL, err := url.Parse(loaded.ProxyURL)
	if err != nil {
		return false, ErrUnavailable
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false, nil
	}
	return strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(proxyURL.Hostname(), ".")) && port == proxyURL.Port(), nil
}

func (service *Service) Transport(base *http.Transport) *http.Transport {
	if base == nil {
		if standard, ok := http.DefaultTransport.(*http.Transport); ok {
			base = standard
		} else {
			base = &http.Transport{}
		}
	}
	transport := base.Clone()
	transport.Proxy = service.Proxy
	return transport
}

// Probe verifies the saved proxy against one fixed, non-sensitive endpoint.
// It never accepts a caller-supplied target, so it cannot become an SSRF oracle.
func (service *Service) Probe(ctx context.Context) (ProbeResult, error) {
	loaded, err := service.load(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	if !loaded.Enabled || loaded.ProxyURL == "" {
		return ProbeResult{}, ErrDisabled
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probeTarget, nil)
	if err != nil {
		return ProbeResult{}, ErrUnavailable
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "beat-egress-probe")
	client := service.probeClient
	if client == nil {
		client = service.newProbeClient()
		defer client.CloseIdleConnections()
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ProbeResult{}, ErrProbeTimeout
		}
		return ProbeResult{}, ErrProbeFailed
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ProbeResult{}, ErrProbeFailed
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, probeBodyLimit))
	if err != nil || !bytes.Contains(body, []byte(probeMarker)) {
		return ProbeResult{}, ErrProbeFailed
	}
	return ProbeResult{CheckedAt: time.Now().UTC(), LatencyMS: time.Since(started).Milliseconds()}, nil
}

func (service *Service) newProbeClient() *http.Client {
	transport := service.Transport(nil)
	transport.DisableKeepAlives = true
	transport.TLSHandshakeTimeout = probeHeaderTimeout
	transport.ResponseHeaderTimeout = probeHeaderTimeout
	transport.MaxResponseHeaderBytes = 64 * 1024
	return &http.Client{Transport: transport, Timeout: probeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}
