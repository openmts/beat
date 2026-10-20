package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestReporterRequiresMatchingCompleteACKAndSanitizesServerFailures(t *testing.T) {
	reporter, err := NewHTTPReporter(Config{ServerURL: "https://private.invalid", AgentToken: "private-token", NodeName: "private-node", AdvertisedHost: "192.0.2.4", SSHPort: 22, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sample := testReportSample(t, model.NodeMetrics{}, nil)
	now := time.Now()
	base := model.AgentReportACK{ProtocolVersion: model.AgentReportProtocol, SampleID: sample.SampleID, CapturedAt: sample.CapturedAt, ContactAccepted: true, MetricsAccepted: true, ContactAt: &now}
	for name, change := range map[string]func(*model.AgentReportACK){
		"wrong sample":         func(ack *model.AgentReportACK) { ack.SampleID = "another-sample" },
		"wrong capture":        func(ack *model.AgentReportACK) { ack.CapturedAt = ack.CapturedAt.Add(time.Nanosecond) },
		"old protocol":         func(ack *model.AgentReportACK) { ack.ProtocolVersion = 1 },
		"contact only":         func(ack *model.AgentReportACK) { ack.MetricsAccepted = false },
		"missing contact":      func(ack *model.AgentReportACK) { ack.ContactAccepted = false },
		"missing contact time": func(ack *model.AgentReportACK) { ack.ContactAt = nil },
		"retry flag":           func(ack *model.AgentReportACK) { ack.Retryable = true },
		"error flag":           func(ack *model.AgentReportACK) { ack.ErrorCode = "private-token private-node 25" },
	} {
		t.Run(name, func(t *testing.T) {
			ack := base
			change(&ack)
			payload, encodeErr := json.Marshal(struct {
				Data model.AgentReportACK `json:"data"`
			}{ack})
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			reporter.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header)}, nil
			})
			_, err := reporter.Report(t.Context(), sample)
			var failure *ReportError
			if !errors.As(err, &failure) || !failure.Retryable || failure.Code != "invalid_ack" || strings.Contains(err.Error(), "private") {
				t.Fatal("unmatched ACK discarded pending sample or disclosed details", err)
			}
		})
	}
	reporter.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"data":{"error_code":"private-token private-node"}}`)), Header: http.Header{"Retry-After": []string{"120"}}}, nil
	})
	_, err = reporter.Report(t.Context(), sample)
	var failure *ReportError
	if !errors.As(err, &failure) || !failure.Retryable || failure.RetryAfter != 2*time.Minute || strings.Contains(err.Error(), "private") {
		t.Fatal("server failure was not bounded/sanitized", err)
	}
}

func TestReportBackoffAndRetryAfterRemainBounded(t *testing.T) {
	for _, attempt := range []int{-1, 0, 1, 20, 1 << 30} {
		for _, jitter := range []float64{-1, 0, 0.5, 1, 2} {
			delay := reportRetryDelay(attempt, jitter, 0)
			if delay < minimumReportBackoff/2 || delay > maximumReportBackoff {
				t.Fatal("unbounded retry delay", delay)
			}
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, test := range []struct {
		value string
		want  time.Duration
	}{{"120", 2 * time.Minute}, {"-1", 0}, {"999999999", model.MaximumAgentSampleAge}, {"bad", 0}, {now.Add(time.Minute).Format(http.TimeFormat), time.Minute}} {
		if delay := parseReportRetryAfter(test.value, now); delay != test.want {
			t.Fatal("Retry-After mismatch", test.value, delay)
		}
	}
}

func TestRunnerHonorsRetryAfterAcrossPendingExpiration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		start := time.Now()
		sampler := &countingSystemSampler{}
		calls := 0
		var sampleID string
		reporter := pendingReporterFunc(func(_ context.Context, sample ReportSample) (string, error) {
			calls++
			if calls == 1 {
				sampleID = sample.SampleID
				return "", &ReportError{Retryable: true, RetryAfter: time.Minute}
			}
			if calls == 2 {
				if sample.SampleID != sampleID || sampler.calls != 1 {
					t.Fatal("pending was replaced")
				}
				return "", &ReportError{Retryable: true, RetryAfter: 15 * time.Minute}
			}
			if time.Since(start) != 16*time.Minute || sample.SampleID == sampleID || sampler.calls != 2 {
				t.Fatal("expiration bypassed Retry-After or retained expired sample")
			}
			cancel()
			return "", nil
		})
		if err := NewRunner(RunnerOptions{Interval: time.Second, Collector: NewCollector(sampler, time.Now), Reporter: reporter}).Run(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
