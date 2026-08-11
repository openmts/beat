package agent

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/beat/backend/internal/model"
)

type IPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type NetworkProber struct {
	resolver IPResolver
	now      func() time.Time
}

func NewNetworkProber() *NetworkProber {
	return &NetworkProber{resolver: net.DefaultResolver, now: model.NowUTC}
}

func (p *NetworkProber) Probe(ctx context.Context, task model.NetworkAssignment) model.NetworkProbeResult {
	timeout := time.Duration(task.TimeoutMilliseconds) * time.Millisecond
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := p.now()
	statusCode, err := p.execute(probeCtx, task)
	finished := p.now()
	result := model.NetworkProbeResult{
		TaskID: task.ID, FinishedAt: finished,
		LatencyMS: float64(finished.Sub(started).Nanoseconds()) / float64(time.Millisecond),
		Success:   err == nil, StatusCode: statusCode, ErrorCode: networkProbeErrorCode(err),
	}
	return result
}

func (p *NetworkProber) execute(ctx context.Context, task model.NetworkAssignment) (int, error) {
	switch task.Type {
	case model.NetworkProbeICMP:
		return 0, p.probeICMP(ctx, task.Target, task.IPFamily)
	case model.NetworkProbeTCP:
		return 0, p.probeTCP(ctx, task.Target, task.IPFamily)
	case model.NetworkProbeHTTP:
		return p.probeHTTP(ctx, task.Target, task.IPFamily)
	default:
		return 0, errInvalidProbeTarget
	}
}

var errInvalidProbeTarget = errors.New("invalid probe target")

func (p *NetworkProber) resolve(ctx context.Context, host, family string) (netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		if matchesIPFamily(address, family) {
			return address.Unmap(), nil
		}
		return netip.Addr{}, errInvalidProbeTarget
	}
	network := "ip"
	switch family {
	case model.IPFamilyIPv4:
		network = "ip4"
	case model.IPFamilyIPv6:
		network = "ip6"
	}
	addresses, err := p.resolver.LookupNetIP(ctx, network, host)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, preferIPv6 := range []bool{family != model.IPFamilyIPv4, false} {
		for _, address := range addresses {
			address = address.Unmap()
			if matchesIPFamily(address, family) && address.Is6() == preferIPv6 {
				return address, nil
			}
		}
	}
	return netip.Addr{}, &net.DNSError{Err: "no suitable address", Name: host}
}

func matchesIPFamily(address netip.Addr, family string) bool {
	switch family {
	case model.IPFamilyIPv4:
		return address.Is4()
	case model.IPFamilyIPv6:
		return address.Is6()
	default:
		return address.Is4() || address.Is6()
	}
}

func validateDialAddress(address netip.Addr) error {
	if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return errInvalidProbeTarget
	}
	return nil
}

func networkProbeErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.DeadlineExceeded) || isNetTimeout(err) {
		return "timeout"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		return "permission"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection_refused"
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return "network_unreachable"
	}
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) || strings.Contains(strings.ToLower(err.Error()), "tls") {
		return "tls"
	}
	if errors.Is(err, errInvalidProbeTarget) {
		return "invalid_target"
	}
	if isHTTPStatusError(err) {
		return "http_status"
	}
	return "io"
}

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
