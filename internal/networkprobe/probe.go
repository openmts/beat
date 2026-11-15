package networkprobe

import (
	"context"
	"errors"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	OutcomeSuccess = "success"
	OutcomeLost    = "lost"
	OutcomeFailed  = "failed"
)

type Result struct {
	Kind       string         `json:"kind"`
	StartedAt  time.Time      `json:"startedAt"`
	DurationMs int64          `json:"durationMs"`
	Targets    []TargetResult `json:"targets"`
}

type TargetResult struct {
	Carrier    string         `json:"carrier"`
	Label      string         `json:"label"`
	Host       string         `json:"host"`
	Port       int            `json:"port"`
	Protocol   string         `json:"protocol"`
	Family     string         `json:"family"`
	ResolvedIP string         `json:"resolvedIp"`
	ResolveMs  float64        `json:"resolveMs"`
	Loss       LossMetrics    `json:"loss"`
	Latency    LatencyMetrics `json:"latency"`
	Samples    []ProbeSample  `json:"samples"`
	Status     string         `json:"status"`
	Errors     []string       `json:"errors,omitempty"`
}

type LossMetrics struct {
	Available bool     `json:"available"`
	Sent      int      `json:"sent"`
	Received  int      `json:"received"`
	Lost      int      `json:"lost"`
	Failed    int      `json:"failed"`
	Percent   *float64 `json:"percent"`
}

type LatencyMetrics struct {
	Available   bool     `json:"available"`
	SampleCount int      `json:"sampleCount"`
	MinMs       *float64 `json:"minMs"`
	AvgMs       *float64 `json:"avgMs"`
	MaxMs       *float64 `json:"maxMs"`
	P50Ms       *float64 `json:"p50Ms"`
	P95Ms       *float64 `json:"p95Ms"`
	JitterMs    *float64 `json:"jitterMs"`
}

type ProbeSample struct {
	Index   int      `json:"index"`
	Outcome string   `json:"outcome"`
	RTTMs   *float64 `json:"rttMs"`
	Error   string   `json:"error,omitempty"`
}

type tcpDialer func(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error)

func RunTCP(ctx context.Context, config Config) Result {
	return runTCP(ctx, config, dialTCP)
}

func runTCP(ctx context.Context, config Config, dial tcpDialer) Result {
	startedAt := time.Now().UTC()
	results := make([]TargetResult, len(config.Targets))
	var waitGroup sync.WaitGroup
	for index := range config.Targets {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			results[index] = probeTCP(ctx, config.Targets[index], config.Probe, dial)
		}(index)
	}
	waitGroup.Wait()
	return Result{
		Kind: Kind, StartedAt: startedAt, DurationMs: time.Since(startedAt).Milliseconds(),
		Targets: results,
	}
}

func dialTCP(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
	return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, address)
}

func probeTCP(ctx context.Context, target Target, settings ProbeSettings, dial tcpDialer) TargetResult {
	result := TargetResult{
		Carrier: target.Carrier, Label: target.Label, Host: target.Host, Port: target.Port,
		Protocol: target.Protocol, Family: target.AddressFamily, Status: "failed",
		Samples: make([]ProbeSample, 0, settings.Count),
	}
	address, family, resolveDuration, err := resolveTarget(ctx, target)
	result.ResolveMs = milliseconds(resolveDuration)
	result.Family = family
	if err != nil {
		result.ResolvedIP = ""
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.ResolvedIP = address
	timeout := time.Duration(settings.TimeoutMs) * time.Millisecond
	interval := time.Duration(settings.IntervalMs) * time.Millisecond
	rtts := make([]float64, 0, settings.Count)
	for attempt := 0; attempt < settings.Count; attempt++ {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err.Error())
			break
		}
		result.Loss.Sent++
		startedAt := time.Now()
		connection, dialErr := dial(ctx, "tcp", net.JoinHostPort(address, strconv.Itoa(target.Port)), timeout)
		rtt := time.Since(startedAt)
		sample := ProbeSample{Index: attempt + 1}
		if dialErr != nil {
			sample.Outcome = classifyDialError(dialErr)
			if sample.Outcome == OutcomeLost {
				result.Loss.Lost++
			} else {
				result.Loss.Failed++
			}
			sample.Error = dialErr.Error()
			if len(result.Errors) < 3 {
				result.Errors = append(result.Errors, dialErr.Error())
			}
		} else {
			sample.Outcome = OutcomeSuccess
			value := milliseconds(rtt)
			sample.RTTMs = &value
			result.Loss.Received++
			rtts = append(rtts, value)
			_ = connection.Close()
		}
		result.Samples = append(result.Samples, sample)
		if attempt+1 < settings.Count {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}
	if result.Loss.Sent > 0 {
		lossPercent := float64(result.Loss.Lost) * 100 / float64(result.Loss.Sent)
		result.Loss.Available = true
		result.Loss.Percent = &lossPercent
	}
	result.Latency = summarizeLatency(rtts)
	if result.Loss.Received == 0 {
		result.Status = "failed"
	} else if result.Loss.Lost > 0 || result.Loss.Failed > 0 {
		result.Status = "degraded"
	} else {
		result.Status = "ok"
	}
	return result
}

func classifyDialError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, os.ErrDeadlineExceeded) {
		return OutcomeLost
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return OutcomeLost
	}
	return OutcomeFailed
}

func resolveTarget(ctx context.Context, target Target) (string, string, time.Duration, error) {
	if parsed := net.ParseIP(target.Host); parsed != nil {
		if parsed.To4() != nil {
			return parsed.String(), FamilyIPv4, 0, nil
		}
		return parsed.String(), FamilyIPv6, 0, nil
	}
	startedAt := time.Now()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, target.Host)
	elapsed := time.Since(startedAt)
	if err != nil {
		return "", target.AddressFamily, elapsed, err
	}
	for _, address := range addresses {
		if target.AddressFamily == FamilyIPv4 && address.IP.To4() != nil {
			return address.IP.String(), FamilyIPv4, elapsed, nil
		}
		if target.AddressFamily == FamilyIPv6 && address.IP.To4() == nil {
			return address.IP.String(), FamilyIPv6, elapsed, nil
		}
	}
	if target.AddressFamily == FamilyAuto {
		for _, address := range addresses {
			if address.IP.To4() != nil {
				return address.IP.String(), FamilyIPv4, elapsed, nil
			}
		}
		for _, address := range addresses {
			if address.IP.To4() == nil {
				return address.IP.String(), FamilyIPv6, elapsed, nil
			}
		}
	}
	return "", target.AddressFamily, elapsed, &net.DNSError{Err: "no address matches requested family", Name: target.Host}
}

func summarizeLatency(values []float64) LatencyMetrics {
	if len(values) == 0 {
		return LatencyMetrics{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	var sum float64
	for _, value := range sorted {
		sum += value
	}
	var jitterSum float64
	for index := 1; index < len(values); index++ {
		difference := values[index] - values[index-1]
		if difference < 0 {
			difference = -difference
		}
		jitterSum += difference
	}
	jitter := 0.0
	if len(values) > 1 {
		jitter = jitterSum / float64(len(values)-1)
	}
	minMs := sorted[0]
	avgMs := sum / float64(len(sorted))
	maxMs := sorted[len(sorted)-1]
	p50Ms := percentile(sorted, 50)
	p95Ms := percentile(sorted, 95)
	return LatencyMetrics{
		Available: true, SampleCount: len(values),
		MinMs: &minMs, AvgMs: &avgMs, MaxMs: &maxMs,
		P50Ms: &p50Ms, P95Ms: &p95Ms, JitterMs: &jitter,
	}
}

func percentile(sorted []float64, percent float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	position := percent / 100 * float64(len(sorted)-1)
	lower := int(position)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	fraction := position - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*fraction
}

func milliseconds(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}
