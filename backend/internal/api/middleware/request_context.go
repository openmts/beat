package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type requestContextKey uint8

const (
	requestInfoKey requestContextKey = iota
	requestIDKey
)

type TrustedProxies struct {
	prefixes []netip.Prefix
}

type EffectiveRequestInfo struct {
	Scheme   string
	Host     string
	ClientIP string
}

func ParseTrustedProxies(value string) (TrustedProxies, error) {
	trusted := TrustedProxies{}
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return TrustedProxies{}, fmt.Errorf("parse trusted proxy CIDR %q: %w", raw, err)
		}
		trusted.prefixes = append(trusted.prefixes, prefix.Masked())
	}
	return trusted, nil
}

func (trusted TrustedProxies) Contains(value string) bool {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	for _, prefix := range trusted.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func RequestContext(trusted TrustedProxies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			info := directRequestInfo(request)
			if trusted.Contains(info.ClientIP) {
				info = forwardedRequestInfo(request, info, trusted)
			}
			id := uuid.NewString()
			ctx := context.WithValue(request.Context(), requestInfoKey, info)
			ctx = context.WithValue(ctx, requestIDKey, id)
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, request.WithContext(ctx))
		})
	}
}

func RequestInfoFrom(request *http.Request) EffectiveRequestInfo {
	if info, ok := request.Context().Value(requestInfoKey).(EffectiveRequestInfo); ok {
		return info
	}
	return directRequestInfo(request)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func IsSecure(request *http.Request) bool {
	return RequestInfoFrom(request).Scheme == "https"
}

func ClientIP(request *http.Request) string {
	return RequestInfoFrom(request).ClientIP
}

func directRequestInfo(request *http.Request) EffectiveRequestInfo {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return EffectiveRequestInfo{
		Scheme: scheme, Host: request.Host, ClientIP: parseRemoteIP(request.RemoteAddr),
	}
}

func forwardedRequestInfo(
	request *http.Request,
	info EffectiveRequestInfo,
	trusted TrustedProxies,
) EffectiveRequestInfo {
	forwarded := lastHeaderElement(request.Header.Get("Forwarded"))
	if forwarded != "" {
		values := parseForwardedElement(forwarded)
		if scheme := validScheme(values["proto"]); scheme != "" {
			info.Scheme = scheme
		}
		if host := validHost(values["host"]); host != "" {
			info.Host = host
		}
		if clientIP := parseForwardedIP(values["for"]); clientIP != "" {
			info.ClientIP = clientIP
		}
		return info
	}
	if scheme := validScheme(lastCommaValue(request.Header.Get("X-Forwarded-Proto"))); scheme != "" {
		info.Scheme = scheme
	}
	if host := validHost(lastCommaValue(request.Header.Get("X-Forwarded-Host"))); host != "" {
		info.Host = host
	}
	if clientIP := forwardedClientIP(request.Header.Get("X-Forwarded-For"), trusted); clientIP != "" {
		info.ClientIP = clientIP
	}
	return info
}

func parseRemoteIP(remoteAddress string) string {
	value := strings.TrimSpace(remoteAddress)
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}

func lastHeaderElement(value string) string {
	elements := splitHeader(value, ',')
	if len(elements) == 0 {
		return ""
	}
	return strings.TrimSpace(elements[len(elements)-1])
}

func parseForwardedElement(value string) map[string]string {
	result := make(map[string]string)
	for _, part := range splitHeader(value, ';') {
		name, raw, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		raw = strings.TrimSpace(raw)
		if unquoted, err := strconv.Unquote(raw); err == nil {
			raw = unquoted
		}
		result[name] = raw
	}
	return result
}

func splitHeader(value string, separator rune) []string {
	parts := []string{}
	start := 0
	quoted := false
	escaped := false
	for index, current := range value {
		if escaped {
			escaped = false
			continue
		}
		if current == '\\' && quoted {
			escaped = true
			continue
		}
		if current == '"' {
			quoted = !quoted
			continue
		}
		if current == separator && !quoted {
			parts = append(parts, value[start:index])
			start = index + 1
		}
	}
	if start <= len(value) {
		parts = append(parts, value[start:])
	}
	return parts
}

func lastCommaValue(value string) string {
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func validScheme(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "http" || value == "https" {
		return value
	}
	return ""
}

func validHost(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse("//" + value)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" {
		return ""
	}
	return parsed.Host
}

func parseForwardedIP(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "unknown") || strings.HasPrefix(value, "_") {
		return ""
	}
	return parseIPWithOptionalPort(value)
}

func forwardedClientIP(value string, trusted TrustedProxies) string {
	parts := strings.Split(value, ",")
	lastValid := ""
	for index := len(parts) - 1; index >= 0; index-- {
		candidate := parseIPWithOptionalPort(strings.TrimSpace(parts[index]))
		if candidate == "" {
			continue
		}
		lastValid = candidate
		if !trusted.Contains(candidate) {
			return candidate
		}
	}
	return lastValid
}

func parseIPWithOptionalPort(value string) string {
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	address, err := netip.ParseAddr(value)
	if err != nil {
		return ""
	}
	return address.String()
}
