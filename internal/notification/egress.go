package notification

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

type IPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type DialContext func(context.Context, string, string) (net.Conn, error)

type Egress struct {
	Resolver IPResolver
	Dial     DialContext
	Now      func() time.Time
}

func NewEgress() *Egress {
	return &Egress{Resolver: net.DefaultResolver, Dial: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, Now: time.Now}
}

type transportFailure struct {
	code, stage string
	retryable   bool
}

func (failure transportFailure) Error() string { return failure.code + ": " + failure.stage }

func (failure transportFailure) Is(target error) bool {
	return target == ErrProviderConfig && failure.stage == "validation" && !failure.retryable
}

func (egress *Egress) Validate(ctx context.Context, endpoint string, policy model.NotificationEgressPolicy) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" || !validProviderScalar(endpoint, maximumEndpointBytes) {
		return transportFailure{code: "endpoint_invalid", stage: "validation"}
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return transportFailure{code: "endpoint_invalid", stage: "validation"}
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
		if parsed.Scheme == "http" {
			port = "80"
		}
	}
	addresses, err := egress.resolve(ctx, net.JoinHostPort(parsed.Hostname(), port), policy)
	if err != nil {
		return err
	}
	if parsed.Scheme == "http" {
		for _, address := range addresses {
			if !address.IsLoopback() {
				return transportFailure{code: "plaintext_forbidden", stage: "validation"}
			}
		}
	}
	return nil
}

func (egress *Egress) resolve(ctx context.Context, address string, policy model.NotificationEgressPolicy) ([]netip.Addr, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, transportFailure{code: "endpoint_invalid", stage: "validation"}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, transportFailure{code: "endpoint_invalid", stage: "validation"}
	}
	host, err = canonicalHost(host)
	if err != nil {
		return nil, transportFailure{code: "endpoint_invalid", stage: "validation"}
	}
	addresses := []netip.Addr{}
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, literal)
	} else {
		addresses, err = egress.Resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, transportFailure{code: "dns_failure", stage: "dns", retryable: true}
		}
	}
	if len(addresses) == 0 || len(addresses) > 32 {
		return nil, transportFailure{code: "dns_failure", stage: "dns", retryable: true}
	}
	for index, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || address.Zone() != "" || hardDeniedAddress(address) {
			return nil, transportFailure{code: "egress_denied", stage: "validation"}
		}
		if !publicAddress(address) && !egress.allowed(policy, resolvedAddress{host: host, port: port, address: address}) {
			return nil, transportFailure{code: "egress_denied", stage: "validation"}
		}
		addresses[index] = address
	}
	return addresses, nil
}

func canonicalHost(host string) (string, error) {
	if host == "" || strings.ContainsAny(host, "%/\\@ \t\r\n") {
		return "", errors.New("invalid host")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String(), nil
	}
	canonical := strings.TrimSuffix(strings.ToLower(host), ".")
	if len(canonical) > 253 || canonical == "" {
		return "", errors.New("invalid host")
	}
	for _, label := range strings.Split(canonical, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("invalid host")
		}
		for _, character := range label {
			valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-'
			if !valid {
				return "", errors.New("invalid host")
			}
		}
	}
	return canonical, nil
}

type resolvedAddress struct {
	host    string
	port    int
	address netip.Addr
}

func (egress *Egress) allowed(policy model.NotificationEgressPolicy, target resolvedAddress) bool {
	for _, exception := range policy.Exceptions {
		if !validExceptionScope(exception) {
			continue
		}
		if exception.Port != target.port || !egress.Now().Before(exception.ExpiresAt) {
			continue
		}
		if exception.Host != "" {
			canonical, err := canonicalHost(exception.Host)
			if err == nil && canonical == target.host {
				return true
			}
		}
		if exception.CIDR != "" {
			prefix, err := netip.ParsePrefix(exception.CIDR)
			if err == nil && prefix.Contains(target.address) {
				return true
			}
		}
	}
	return false
}

func validExceptionScope(exception model.NotificationEgressException) bool {
	if exception.Port < 1 || exception.Port > 65535 || (exception.Host == "") == (exception.CIDR == "") {
		return false
	}
	if exception.Host != "" {
		_, err := canonicalHost(exception.Host)
		return err == nil
	}
	prefix, err := netip.ParsePrefix(exception.CIDR)
	if err != nil || prefix != prefix.Masked() {
		return false
	}
	if prefix.Addr().Is4() {
		return prefix.Bits() >= 24
	}
	return prefix.Bits() >= 64
}

type DialTarget struct {
	Network, Address string
	Policy           model.NotificationEgressPolicy
	LoopbackOnly     bool
}

func (egress *Egress) DialChecked(ctx context.Context, target DialTarget) (net.Conn, error) {
	addresses, err := egress.resolve(ctx, target.Address, target.Policy)
	if err != nil {
		return nil, err
	}
	_, port, err := net.SplitHostPort(target.Address)
	if err != nil {
		return nil, transportFailure{code: "endpoint_invalid", stage: "validation"}
	}
	for _, ip := range addresses {
		if target.LoopbackOnly && !ip.IsLoopback() {
			return nil, transportFailure{code: "plaintext_forbidden", stage: "validation"}
		}
	}
	for _, ip := range addresses {
		connection, err := egress.Dial(ctx, target.Network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return connection, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, transportFailure{code: "connect_failed", stage: "connect", retryable: true}
}

var deniedPrefixes = func() []netip.Prefix {
	values := []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8", "2001:db8::/32", "2001::/32", "2001:10::/28", "2001:20::/28", "2002::/16"}
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func hardDeniedAddress(address netip.Addr) bool {
	return address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address == netip.MustParseAddr("100.100.100.200")
}

func publicAddress(address netip.Addr) bool {
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range deniedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func ValidateEgressPolicy(policy model.NotificationEgressPolicy, now time.Time) error {
	if len(policy.Exceptions) > 64 {
		return ErrProviderConfig
	}
	for _, exception := range policy.Exceptions {
		if exception.Port < 1 || exception.Port > 65535 || exception.Purpose == "" || len(exception.Purpose) > 200 || !now.Before(exception.ExpiresAt) || exception.ExpiresAt.After(now.Add(90*24*time.Hour)) {
			return ErrProviderConfig
		}
		if (exception.Host == "") == (exception.CIDR == "") {
			return ErrProviderConfig
		}
		if exception.Host != "" {
			if strings.Contains(exception.Host, "*") {
				return ErrProviderConfig
			}
			if _, err := canonicalHost(exception.Host); err != nil {
				return ErrProviderConfig
			}
		}
		if exception.CIDR != "" {
			prefix, err := netip.ParsePrefix(exception.CIDR)
			if err != nil || prefix != prefix.Masked() {
				return ErrProviderConfig
			}
			minimum := 64
			if prefix.Addr().Is4() {
				minimum = 24
			}
			if prefix.Bits() < minimum {
				return ErrProviderConfig
			}
		}
	}
	return nil
}
