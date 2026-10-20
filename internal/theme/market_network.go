package theme

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

const maximumMarketAddresses = 32

type marketNetwork struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func marketDialContext(dialer *net.Dialer, allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	return checkedMarketDial(marketNetwork{lookup: net.DefaultResolver.LookupIPAddr, dial: dialer.DialContext}, allowPrivate)
}

// DNS 校验后的地址直接用于拨号，禁止再用原域名解析一次。
func checkedMarketDial(policy marketNetwork, allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrMarketInvalid
		}
		addresses, err := policy.lookup(ctx, host)
		if err != nil {
			return nil, errors.Join(ErrMarketUnavailable, ctx.Err())
		}
		if len(addresses) == 0 || len(addresses) > maximumMarketAddresses {
			return nil, ErrMarketInvalid
		}
		for _, address := range addresses {
			if address.Zone != "" || address.IP == nil || !allowPrivate && !publicMarketIP(address.IP) {
				return nil, ErrMarketInvalid
			}
		}
		var lastError error
		for _, address := range addresses {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			connection, err := policy.dial(ctx, network, net.JoinHostPort(address.IP.String(), port))
			if err == nil {
				return connection, nil
			}
			lastError = err
		}
		return nil, fmt.Errorf("%w: checked destinations were unreachable: %w", ErrMarketUnavailable, lastError)
	}
}

func validateMarketURL(target *url.URL, allowPrivate bool) error {
	if target == nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" || target.Opaque != "" || target.Hostname() == "" || strings.Contains(target.Hostname(), "%") {
		return ErrMarketInvalid
	}
	if literal := net.ParseIP(target.Hostname()); literal != nil && !allowPrivate && !publicMarketIP(literal) {
		return ErrMarketInvalid
	}
	return nil
}

var specialMarketNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("fec0::/10"),
}

var marketIPv6GlobalUnicast = netip.MustParsePrefix("2000::/3")

func publicMarketIP(ip net.IP) bool {
	address, valid := netip.AddrFromSlice(ip)
	if !valid {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	// IPv6 只允许已分配的全球单播空间，排除 IPv4 兼容、丢弃和未分配前缀。
	if !address.Is4() && !marketIPv6GlobalUnicast.Contains(address) {
		return false
	}
	for _, prefix := range specialMarketNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
