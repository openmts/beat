package agent

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeIPResolver struct {
	addresses []netip.Addr
	err       error
}

func isICMPPrivilegeError(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES)
}

func (resolver fakeIPResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return resolver.addresses, resolver.err
}

func TestNetworkProberHTTP(t *testing.T) {
	redirected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			w.Header().Set("Location", "/target")
			w.WriteHeader(http.StatusFound)
		case "/target":
			redirected = true
			w.WriteHeader(http.StatusNoContent)
		case "/failure":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			if r.Header.Get("Accept-Encoding") != "identity" {
				t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	prober := NewNetworkProber()
	for _, test := range []struct {
		name      string
		path      string
		success   bool
		status    int
		errorCode string
	}{
		{name: "success", success: true, status: http.StatusNoContent, errorCode: "none"},
		{name: "redirect", path: "/redirect", success: true, status: http.StatusFound, errorCode: "none"},
		{name: "failure", path: "/failure", status: http.StatusServiceUnavailable, errorCode: "http_status"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := prober.Probe(t.Context(), networkAssignment(model.NetworkProbeHTTP, server.URL+test.path))
			if result.Success != test.success || result.StatusCode != test.status || result.ErrorCode != test.errorCode {
				t.Fatalf("result = %#v", result)
			}
		})
	}
	if redirected {
		t.Fatal("HTTP probe followed redirect")
	}
}

func TestNetworkProberTCPAndErrors(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = connection.Close()
		}
		close(done)
	}()
	prober := NewNetworkProber()
	result := prober.Probe(t.Context(), networkAssignment(model.NetworkProbeTCP, listener.Addr().String()))
	if !result.Success || result.ErrorCode != "none" {
		t.Fatalf("TCP success result = %#v", result)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	<-done

	result = prober.Probe(t.Context(), networkAssignment(model.NetworkProbeTCP, listener.Addr().String()))
	if result.Success || result.ErrorCode != "connection_refused" {
		t.Fatalf("TCP refused result = %#v", result)
	}
	result = prober.Probe(t.Context(), networkAssignment(model.NetworkProbeTCP, "invalid"))
	if result.Success || result.ErrorCode != "invalid_target" {
		t.Fatalf("TCP invalid result = %#v", result)
	}
}

func TestNetworkProberICMPLoopback(t *testing.T) {
	prober := NewNetworkProber()
	tests := []struct {
		name   string
		target string
		family string
	}{
		{name: "IPv4", target: "127.0.0.1", family: model.IPFamilyIPv4},
		{name: "IPv6", target: "::1", family: model.IPFamilyIPv6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			stats, err := prober.probeICMP(ctx, test.target, test.family, 3)
			if err != nil {
				if isICMPPrivilegeError(err) {
					t.Skipf("ICMP socket unavailable in this environment: %v", err)
				}
				t.Fatalf("probe ICMP %s: %v", test.target, err)
			}
			if stats.sent != 3 || stats.received != 3 || len(stats.rtts) != 3 {
				t.Fatalf("ICMP stats = %#v", stats)
			}
		})
	}
}

func TestApplyICMPStats(t *testing.T) {
	t.Run("partial loss and jitter", func(t *testing.T) {
		result := model.NetworkProbeResult{LatencyMS: 999}
		applyICMPStats(&result, icmpProbeStats{
			sent: 4, received: 2, rtts: []time.Duration{10 * time.Millisecond, 30 * time.Millisecond},
		})
		if result.SentPackets != 4 || result.ReceivedPackets != 2 || result.LossPercent == nil || *result.LossPercent != 50 {
			t.Fatalf("packet stats = %#v", result)
		}
		if result.LatencyMS != 20 || result.MinLatencyMS == nil || *result.MinLatencyMS != 10 ||
			result.MaxLatencyMS == nil || *result.MaxLatencyMS != 30 ||
			result.JitterMS == nil || *result.JitterMS != 20 {
			t.Fatalf("latency stats = %#v", result)
		}
	})

	t.Run("zero packets keeps latency and omits loss", func(t *testing.T) {
		result := model.NetworkProbeResult{LatencyMS: 12}
		applyICMPStats(&result, icmpProbeStats{})
		if result.SentPackets != 0 || result.ReceivedPackets != 0 || result.LossPercent != nil || result.LatencyMS != 12 {
			t.Fatalf("empty stats = %#v", result)
		}
	})

	t.Run("single reply has zero jitter", func(t *testing.T) {
		result := model.NetworkProbeResult{}
		applyICMPStats(&result, icmpProbeStats{sent: 3, received: 1, rtts: []time.Duration{15 * time.Millisecond}})
		if result.LossPercent == nil || math.Abs(*result.LossPercent-200.0/3) > 1e-9 || result.JitterMS == nil || *result.JitterMS != 0 {
			t.Fatalf("single reply stats = %#v", result)
		}
	})
}

func TestProbeAttemptsTracksPartialSuccessAndStatistics(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	prober := &NetworkProber{now: func() time.Time {
		now = now.Add(10 * time.Millisecond)
		return now
	}}
	calls := 0
	observation := prober.probeAttempts(t.Context(), "example.com:443", model.IPFamilyAuto, 3,
		func(context.Context, string, string) (int, error) {
			calls++
			if calls == 2 {
				return 0, errors.New("connection refused")
			}
			return http.StatusNoContent, nil
		})
	if observation.attempts != 3 || observation.successes != 2 || observation.statusCode != http.StatusNoContent ||
		observation.err != nil || len(observation.rtts) != 2 {
		t.Fatalf("partial observation = %#v", observation)
	}
	result := model.NetworkProbeResult{FinishedAt: now}
	applyAttemptStats(&result, observation.attempts, observation.successes, observation.rtts)
	if result.AttemptCount != 3 || result.SuccessCount != 2 || result.SuccessPercent == nil ||
		*result.SuccessPercent != float64(2)/float64(3)*100 || result.P95LatencyMS == nil || result.StdDevLatencyMS == nil {
		t.Fatalf("attempt result = %#v", result)
	}

	failed := prober.probeAttempts(t.Context(), "example.com:443", model.IPFamilyAuto, 2,
		func(context.Context, string, string) (int, error) { return 0, errors.New("timeout") })
	if failed.attempts != 2 || failed.successes != 0 || failed.err == nil || len(failed.rtts) != 0 {
		t.Fatalf("failed observation = %#v", failed)
	}
}

func TestProbeAttemptsStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	observation := (&NetworkProber{now: model.NowUTC}).probeAttempts(ctx, "example.com:443",
		model.IPFamilyAuto, 5, func(context.Context, string, string) (int, error) {
			t.Fatal("attempt should not run after cancellation")
			return 0, nil
		})
	if observation.attempts != 0 || !errors.Is(observation.err, context.Canceled) {
		t.Fatalf("cancelled observation = %#v", observation)
	}
}

func TestNetworkProberResolutionAndValidation(t *testing.T) {
	prober := &NetworkProber{
		resolver: fakeIPResolver{addresses: []netip.Addr{
			netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("2001:db8::1"),
		}},
		now: model.NowUTC,
	}
	address, err := prober.resolve(t.Context(), "example.test", model.IPFamilyAuto)
	if err != nil || !address.Is6() {
		t.Fatalf("auto address = %v, error = %v", address, err)
	}
	address, err = prober.resolve(t.Context(), "example.test", model.IPFamilyIPv4)
	if err != nil || !address.Is4() {
		t.Fatalf("IPv4 address = %v, error = %v", address, err)
	}
	prober.resolver = fakeIPResolver{err: &net.DNSError{Err: "missing", Name: "missing.test"}}
	result := prober.Probe(t.Context(), networkAssignment(model.NetworkProbeICMP, "missing.test"))
	if result.ErrorCode != "dns" {
		t.Fatalf("DNS result = %#v", result)
	}
	result = prober.Probe(t.Context(), networkAssignment(model.NetworkProbeHTTP, "http://0.0.0.0"))
	if result.ErrorCode != "invalid_target" {
		t.Fatalf("unspecified result = %#v", result)
	}
}

func TestNetworkProberTLS(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer tlsServer.Close()
	result := NewNetworkProber().Probe(t.Context(), networkAssignment(model.NetworkProbeHTTP, tlsServer.URL))
	if result.Success || result.ErrorCode != "tls" {
		t.Fatalf("TLS result = %#v", result)
	}
}

func TestNetworkProbeErrorCodes(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: nil, want: "none"},
		{err: context.DeadlineExceeded, want: "timeout"},
		{err: errInvalidProbeTarget, want: "invalid_target"},
		{err: syscall.EPERM, want: "permission"},
		{err: syscall.ENETUNREACH, want: "network_unreachable"},
		{err: errors.New("TLS handshake failed"), want: "tls"},
		{err: errors.New("HTTP status outside success range"), want: "http_status"},
		{err: errors.New("plain I/O"), want: "io"},
	}
	for _, test := range tests {
		if got := networkProbeErrorCode(test.err); got != test.want {
			t.Fatalf("networkProbeErrorCode(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}

func TestNetworkProbeAdditionalBranches(t *testing.T) {
	prober := &NetworkProber{
		resolver: fakeIPResolver{addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}},
		now:      model.NowUTC,
	}
	if observation := prober.execute(t.Context(), networkAssignment("unknown", "target")); !errors.Is(observation.err, errInvalidProbeTarget) {
		t.Fatalf("unknown probe error = %v", observation.err)
	}
	if _, err := prober.resolve(t.Context(), "127.0.0.1", model.IPFamilyIPv6); !errors.Is(err, errInvalidProbeTarget) {
		t.Fatalf("literal family error = %v", err)
	}
	if _, err := prober.resolve(t.Context(), "example.test", model.IPFamilyIPv6); err == nil {
		t.Fatal("expected no suitable address error")
	}
	if !matchesIPFamily(netip.MustParseAddr("127.0.0.1"), model.IPFamilyAuto) {
		t.Fatal("auto family should accept IPv4")
	}
	if _, err := prober.probeICMP(t.Context(), "0.0.0.0", model.IPFamilyIPv4, 1); !errors.Is(err, errInvalidProbeTarget) {
		t.Fatalf("unspecified ICMP error = %v", err)
	}
	if _, err := prober.probeHTTP(t.Context(), "ftp://example.test", model.IPFamilyAuto); !errors.Is(err, errInvalidProbeTarget) {
		t.Fatalf("invalid HTTP error = %v", err)
	}
	if _, err := prober.probeHTTP(t.Context(), "https://127.0.0.1", model.IPFamilyIPv4); err == nil {
		t.Fatal("expected default HTTPS port connection error")
	}
}

func networkAssignment(probeType, target string) model.NetworkAssignment {
	return model.NetworkAssignment{
		ID: "task", Name: strings.ToUpper(probeType), Type: probeType, Target: target,
		IPFamily: model.IPFamilyAuto, IntervalSeconds: 60, TimeoutMilliseconds: int(time.Second / time.Millisecond),
	}
}
