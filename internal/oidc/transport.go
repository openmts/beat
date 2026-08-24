package oidc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxProviderResponseBytes = 1 << 20 // 1 MiB
	dialTimeout              = 10 * time.Second
	handshakeTimeout         = 10 * time.Second
	headerTimeout            = 10 * time.Second
	maxRedirects             = 3
)

// EndpointPolicy 控制 OIDC 外发目标的地址策略。
type EndpointPolicy struct {
	// AllowPrivate 允许回环/私有地址；仅用于注入的本地测试提供方。
	AllowPrivate bool
}

// ValidateIssuer 校验发行人 URL 满足规范约束。
func (policy EndpointPolicy) ValidateIssuer(raw string) error {
	if err := validateProviderURL(raw); err != nil {
		return err
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse issuer URL: %w", err)
	}
	if policy.AllowPrivate {
		return nil
	}
	return validatePublicHost(parsed.Hostname())
}

// newHTTPClient 构造 SSRF 安全的 HTTP 客户端，并强制响应体大小上限。
func (policy EndpointPolicy) newHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           policy.dialContext(dialer),
		TLSHandshakeTimeout:   handshakeTimeout,
		ResponseHeaderTimeout: headerTimeout,
		DisableCompression:    false,
		ForceAttemptHTTP2:     false,
	}
	client := &http.Client{
		Transport: &boundedTransport{inner: transport},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("oidc: too many redirects")
			}
			return policy.ValidateRequestURL(request.URL)
		},
	}
	return client
}

// boundedTransport 包装内部传输，使超过上限的响应体读取返回错误。
type boundedTransport struct {
	inner http.RoundTripper
}

func (t *boundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.inner.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &boundedBody{reader: response.Body, remaining: maxProviderResponseBytes + 1}
	return response, nil
}

type boundedBody struct {
	reader    io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(buffer []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("oidc: provider response exceeds size limit")
	}
	if int64(len(buffer)) > b.remaining {
		buffer = buffer[:b.remaining]
	}
	read, err := b.reader.Read(buffer)
	b.remaining -= int64(read)
	return read, err
}

func (b *boundedBody) Close() error { return b.reader.Close() }

// ValidateRequestURL 校验外发请求目标 URL：仅 https、无用户信息、公开地址。
func (policy EndpointPolicy) ValidateRequestURL(target *url.URL) error {
	if target == nil {
		return errors.New("oidc: missing request url")
	}
	if target.User != nil {
		return errors.New("oidc: request url must not contain user info")
	}
	if target.Scheme != "https" {
		return errors.New("oidc: request url must use https")
	}
	if target.Fragment != "" {
		return errors.New("oidc: request url must not contain a fragment")
	}
	if policy.AllowPrivate {
		return nil
	}
	return validatePublicHost(target.Hostname())
}

func (policy EndpointPolicy) dialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("oidc: split dial target: %w", err)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("oidc: resolve dial target: %w", err)
		}
		if len(ips) == 0 {
			return nil, errors.New("oidc: dial target resolved to no addresses")
		}
		for _, ip := range ips {
			if err := policy.validateIP(ip.IP); err != nil {
				return nil, err
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
}

func (policy EndpointPolicy) validateIP(ip net.IP) error {
	if policy.AllowPrivate {
		return nil
	}
	if ip == nil {
		return errors.New("oidc: dial target has no ip address")
	}
	// IsGlobalUnicast 不排除私网地址，需同时拒绝 IsPrivate。
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return fmt.Errorf("oidc: dial target %s is not a public unicast address", ip)
	}
	return nil
}

func validatePublicHost(host string) error {
	if host == "" {
		return errors.New("oidc: issuer url has no host")
	}
	if literal := net.ParseIP(host); literal != nil {
		if !literal.IsGlobalUnicast() || literal.IsPrivate() {
			return fmt.Errorf("oidc: issuer address %s is not public", literal)
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("oidc: resolve issuer host: %w", err)
	}
	for _, resolved := range ips {
		if !resolved.IP.IsGlobalUnicast() || resolved.IP.IsPrivate() {
			return fmt.Errorf("oidc: issuer host %s resolves to non-public address %s", host, resolved.IP)
		}
	}
	return nil
}

func validateProviderURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse provider url: %w", err)
	}
	if parsed.Scheme != "https" {
		return errors.New("oidc: provider url must use https")
	}
	if parsed.User != nil {
		return errors.New("oidc: provider url must not contain user info")
	}
	if parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("oidc: provider url must not contain a fragment or query")
	}
	return nil
}

// normalizeIssuer 移除尾部斜杠，保证 issuer 一致性比较。
func normalizeIssuer(raw string) string {
	return strings.TrimRight(raw, "/")
}
