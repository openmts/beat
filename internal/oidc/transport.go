package oidc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	maxProviderResponseBytes = 1 << 20 // 1 MiB
	dialTimeout              = 10 * time.Second
	handshakeTimeout         = 10 * time.Second
	headerTimeout            = 10 * time.Second
	providerRequestTimeout   = 30 * time.Second
	maxRedirects             = 3
	maximumOIDCAddresses     = 32
)

// EndpointPolicy 控制 OIDC 外发目标的地址策略。
type EndpointPolicy struct {
	// AllowPrivate 允许回环/私有地址；仅用于注入的本地测试提供方。
	AllowPrivate   bool
	Proxy          func(*http.Request) (*url.URL, error)
	AllowProxyDial func(context.Context, string) (bool, error)
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
	proxy := policy.Proxy
	if proxy != nil {
		baseProxy := proxy
		proxy = func(request *http.Request) (*url.URL, error) {
			if request == nil || request.URL == nil {
				return nil, errors.New("oidc: missing proxied request url")
			}
			// HTTP/SOCKS 代理会自行解析目标域名，不会经过 DialContext。交给代理前
			// 仍执行同一套 URL 与公网地址校验，避免发现出的私网 Token/JWKS URL 绕过。
			if err := policy.validateRequestURL(request.Context(), request.URL); err != nil {
				return nil, fmt.Errorf("oidc: validate proxy target: %w", err)
			}
			return baseProxy(request)
		}
	}
	transport := &http.Transport{
		Proxy:                 proxy,
		DialContext:           policy.dialContext(dialer),
		TLSHandshakeTimeout:   handshakeTimeout,
		ResponseHeaderTimeout: headerTimeout,
		DisableCompression:    false,
		ForceAttemptHTTP2:     false,
	}
	client := &http.Client{
		Transport: &boundedTransport{inner: transport},
		Timeout:   providerRequestTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("oidc: too many redirects")
			}
			if len(via) == 0 || !sameOriginURL(via[0].URL, request.URL) {
				return errors.New("oidc: redirect must remain on the original origin")
			}
			return policy.ValidateRequestURL(request.URL)
		},
	}
	return client
}

func sameOriginURL(first, second *url.URL) bool {
	if first == nil || second == nil {
		return false
	}
	return strings.EqualFold(first.Scheme, second.Scheme) &&
		strings.EqualFold(first.Hostname(), second.Hostname()) &&
		effectivePort(first) == effectivePort(second)
}

func effectivePort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	switch strings.ToLower(target.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
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
	return policy.validateRequestURL(context.Background(), target)
}

func (policy EndpointPolicy) validateRequestURL(ctx context.Context, target *url.URL) error {
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
	return validatePublicHostContext(ctx, target.Hostname())
}

func (policy EndpointPolicy) dialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return policy.checkedDial(oidcDialNetwork{lookup: net.DefaultResolver.LookupIPAddr, dial: dialer.DialContext})
}

type oidcDialNetwork struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (policy EndpointPolicy) checkedDial(network oidcDialNetwork) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, networkName, address string) (net.Conn, error) {
		if policy.AllowProxyDial != nil {
			allowed, allowErr := policy.AllowProxyDial(ctx, address)
			if allowErr != nil {
				return nil, fmt.Errorf("oidc: check proxy address: %w", allowErr)
			}
			if allowed {
				return network.dial(ctx, networkName, address)
			}
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("oidc: split dial target: %w", err)
		}
		ips, err := network.lookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("oidc: resolve dial target: %w", err)
		}
		if len(ips) == 0 || len(ips) > maximumOIDCAddresses {
			return nil, errors.New("oidc: dial target resolved to an invalid number of addresses")
		}
		for _, ip := range ips {
			if ip.Zone != "" {
				return nil, errors.New("oidc: dial target has an IPv6 zone")
			}
			if err := policy.validateIP(ip.IP); err != nil {
				return nil, err
			}
		}
		var lastError error
		for _, ip := range ips {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			connection, dialErr := network.dial(ctx, networkName, net.JoinHostPort(ip.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastError = dialErr
		}
		return nil, fmt.Errorf("oidc: checked destinations were unreachable: %w", lastError)
	}
}

func (policy EndpointPolicy) validateIP(ip net.IP) error {
	if policy.AllowPrivate {
		return nil
	}
	if ip == nil {
		return errors.New("oidc: dial target has no ip address")
	}
	if !publicOIDCAddress(ip) {
		return fmt.Errorf("oidc: dial target %s is not a public unicast address", ip)
	}
	return nil
}

var oidcSpecialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"), netip.MustParsePrefix("fec0::/10"),
}

var oidcGlobalIPv6 = netip.MustParsePrefix("2000::/3")

func publicOIDCAddress(ip net.IP) bool {
	address, valid := netip.AddrFromSlice(ip)
	if !valid {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	// IPv6 只允许全球单播空间，排除 IPv4 兼容、过渡和未分配前缀。
	if !address.Is4() && !oidcGlobalIPv6.Contains(address) {
		return false
	}
	for _, prefix := range oidcSpecialNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validatePublicHost(host string) error {
	return validatePublicHostContext(context.Background(), host)
}

func validatePublicHostContext(ctx context.Context, host string) error {
	return validatePublicHostWithLookup(ctx, host, net.DefaultResolver.LookupIPAddr)
}

func validatePublicHostWithLookup(
	ctx context.Context,
	host string,
	lookup func(context.Context, string) ([]net.IPAddr, error),
) error {
	if host == "" {
		return errors.New("oidc: issuer url has no host")
	}
	if literal := net.ParseIP(host); literal != nil {
		if !publicOIDCAddress(literal) {
			return fmt.Errorf("oidc: issuer address %s is not public", literal)
		}
		return nil
	}
	ips, err := lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("oidc: resolve issuer host: %w", err)
	}
	if len(ips) == 0 || len(ips) > maximumOIDCAddresses {
		return errors.New("oidc: issuer host resolved to an invalid number of addresses")
	}
	for _, resolved := range ips {
		if resolved.IP == nil || !publicOIDCAddress(resolved.IP) {
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
