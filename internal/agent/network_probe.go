package agent

import (
	"context"
	"crypto/tls"
	"errors"
	"math"
	"net"
	"net/netip"
	"sort"
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

type icmpProbeStats struct {
	sent     int
	received int
	rtts     []time.Duration
}

type probeObservation struct {
	statusCode int
	sent       int
	received   int
	attempts   int
	successes  int
	rtts       []time.Duration
	err        error
}

func NewNetworkProber() *NetworkProber {
	return &NetworkProber{resolver: net.DefaultResolver, now: model.NowUTC}
}

func (p *NetworkProber) Probe(ctx context.Context, task model.NetworkAssignment) model.NetworkProbeResult {
	timeout := time.Duration(task.TimeoutMilliseconds) * time.Millisecond
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := p.now()
	observation := p.execute(probeCtx, task)
	finished := p.now()
	result := model.NetworkProbeResult{
		TaskID: task.ID, TargetID: task.TargetID, FinishedAt: finished,
		LatencyMS: float64(finished.Sub(started).Nanoseconds()) / float64(time.Millisecond),
		Success:   observation.successes > 0, StatusCode: observation.statusCode,
		ErrorCode: networkProbeErrorCode(observation.err),
	}
	if result.Success {
		result.ErrorCode = "none"
	}
	if task.Type == model.NetworkProbeICMP {
		applyICMPStats(&result, icmpProbeStats{
			sent: observation.sent, received: observation.received, rtts: observation.rtts,
		})
	} else {
		applyAttemptStats(&result, observation.attempts, observation.successes, observation.rtts)
	}
	return result
}

func (p *NetworkProber) execute(
	ctx context.Context,
	task model.NetworkAssignment,
) probeObservation {
	switch task.Type {
	case model.NetworkProbeICMP:
		stats, err := p.probeICMP(ctx, task.Target, task.IPFamily, task.PacketCount)
		return probeObservation{
			sent: stats.sent, received: stats.received, attempts: stats.sent,
			successes: stats.received, rtts: stats.rtts, err: err,
		}
	case model.NetworkProbeTCP:
		return p.probeAttempts(ctx, task.Target, task.IPFamily, task.PacketCount, func(ctx context.Context, target, family string) (int, error) {
			return 0, p.probeTCP(ctx, target, family)
		})
	case model.NetworkProbeHTTP:
		return p.probeAttempts(ctx, task.Target, task.IPFamily, task.PacketCount, p.probeHTTP)
	default:
		return probeObservation{err: errInvalidProbeTarget}
	}
}

func (p *NetworkProber) probeAttempts(
	ctx context.Context,
	target string,
	family string,
	attemptCount int,
	attempt func(context.Context, string, string) (int, error),
) probeObservation {
	if attemptCount < 1 {
		attemptCount = 1
	}
	if attemptCount > model.MaxNetworkPacketCount {
		attemptCount = model.MaxNetworkPacketCount
	}
	observation := probeObservation{}
	for index := 0; index < attemptCount; index++ {
		if err := ctx.Err(); err != nil {
			observation.err = err
			break
		}
		started := p.now()
		statusCode, err := attempt(ctx, target, family)
		finished := p.now()
		observation.attempts++
		observation.statusCode = statusCode
		if err == nil {
			observation.successes++
			observation.rtts = append(observation.rtts, finished.Sub(started))
		} else {
			observation.err = err
		}
	}
	if observation.successes > 0 {
		observation.err = nil
	}
	return observation
}

func applyICMPStats(result *model.NetworkProbeResult, stats icmpProbeStats) {
	result.SentPackets = stats.sent
	result.ReceivedPackets = stats.received
	result.AttemptCount = stats.sent
	result.SuccessCount = stats.received
	if stats.sent > 0 {
		successPercent := float64(stats.received) / float64(stats.sent) * 100
		result.SuccessPercent = &successPercent
	}
	if stats.sent > 0 {
		loss := float64(stats.sent-stats.received) / float64(stats.sent) * 100
		result.LossPercent = &loss
	}
	applyLatencyStats(result, stats.rtts)
}

func applyAttemptStats(result *model.NetworkProbeResult, attempts, successes int, rtts []time.Duration) {
	result.AttemptCount = attempts
	result.SuccessCount = successes
	if attempts > 0 {
		successPercent := float64(successes) / float64(attempts) * 100
		result.SuccessPercent = &successPercent
	}
	applyLatencyStats(result, rtts)
}

func applyLatencyStats(result *model.NetworkProbeResult, rtts []time.Duration) {
	rtts = append([]time.Duration(nil), rtts...)
	if len(rtts) == 0 {
		return
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	statistics := latencyStatistics(rtts)
	result.LatencyMS = durationMilliseconds(statistics.average)
	minimumMS := durationMilliseconds(statistics.minimum)
	maximumMS := durationMilliseconds(statistics.maximum)
	jitterMS := durationMilliseconds(statistics.jitter)
	p95MS := durationMilliseconds(statistics.p95)
	stddevMS := durationMilliseconds(statistics.stddev)
	result.MinLatencyMS = &minimumMS
	result.MaxLatencyMS = &maximumMS
	result.JitterMS = &jitterMS
	result.P95LatencyMS = &p95MS
	result.StdDevLatencyMS = &stddevMS
}

type latencyStats struct {
	minimum time.Duration
	maximum time.Duration
	average time.Duration
	jitter  time.Duration
	p95     time.Duration
	stddev  time.Duration
}

func latencyStatistics(sortedRTTs []time.Duration) latencyStats {
	statistics := latencyStats{minimum: sortedRTTs[0], maximum: sortedRTTs[len(sortedRTTs)-1]}
	var total time.Duration
	var jitterTotal time.Duration
	for index, rtt := range sortedRTTs {
		total += rtt
		if index > 0 {
			delta := rtt - sortedRTTs[index-1]
			if delta < 0 {
				delta = -delta
			}
			jitterTotal += delta
		}
	}
	statistics.average = total / time.Duration(len(sortedRTTs))
	if len(sortedRTTs) > 1 {
		statistics.jitter = jitterTotal / time.Duration(len(sortedRTTs)-1)
		mean := float64(total) / float64(len(sortedRTTs))
		var variance float64
		for _, rtt := range sortedRTTs {
			delta := float64(rtt) - mean
			variance += delta * delta
		}
		statistics.stddev = time.Duration(math.Sqrt(variance / float64(len(sortedRTTs))))
	}
	p95Index := int(math.Ceil(float64(len(sortedRTTs))*0.95)) - 1
	if p95Index < 0 {
		p95Index = 0
	}
	if p95Index >= len(sortedRTTs) {
		p95Index = len(sortedRTTs) - 1
	}
	statistics.p95 = sortedRTTs[p95Index]
	return statistics
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(value.Nanoseconds()) / float64(time.Millisecond)
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
