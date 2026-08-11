package agent

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const maxProbeResponseHeaders = 64 * 1024

func (p *NetworkProber) probeHTTP(ctx context.Context, target, family string) (int, error) {
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.Fragment != "" {
		return 0, errInvalidProbeTarget
	}
	host := parsed.Hostname()
	address, err := p.resolve(ctx, host, family)
	if err != nil {
		return 0, err
	}
	if err := validateDialAddress(address); err != nil {
		return 0, err
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
	}
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true,
		MaxResponseHeaderBytes: maxProbeResponseHeaders,
		TLSClientConfig:        &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		DialContext: func(dialCtx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(dialCtx, network, net.JoinHostPort(address.String(), port))
		},
	}
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, errInvalidProbeTarget
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		transport.CloseIdleConnections()
		return 0, err
	}
	statusCode := response.StatusCode
	closeErr := response.Body.Close()
	transport.CloseIdleConnections()
	if closeErr != nil {
		return statusCode, fmt.Errorf("close HTTP probe response: %w", closeErr)
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusBadRequest {
		return statusCode, errors.New("HTTP status outside success range")
	}
	return statusCode, nil
}

func isHTTPStatusError(err error) bool {
	return strings.Contains(err.Error(), "HTTP status")
}
