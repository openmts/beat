package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	reportPath            = "/api/v1/nodes/report"
	httpClientTimeout     = 15 * time.Second
	maximumReportACKBytes = 16 << 10
	maxErrorBody          = 4096
)

type NodeReport = model.AgentReport

type HTTPReporter struct {
	accepted          func(time.Time)
	failed            func(error)
	acceleratorPolicy func(model.AcceleratorPolicy) error
	endpoint          string
	token             string
	report            NodeReport
	client            *http.Client
}

func NewHTTPReporter(config Config) (*HTTPReporter, error) {
	serverURL, err := url.Parse(config.ServerURL)
	if err != nil || (serverURL.Scheme != "http" && serverURL.Scheme != "https") || serverURL.Host == "" || serverURL.User != nil {
		return nil, errors.New("invalid server URL")
	}
	serverURL.Path = strings.TrimRight(serverURL.Path, "/") + reportPath
	serverURL.RawQuery, serverURL.Fragment = "", ""
	interval := config.ReportInterval
	if interval == 0 {
		interval = model.MinimumAgentReportInterval
	}
	return &HTTPReporter{endpoint: serverURL.String(), token: config.AgentToken,
		report: NodeReport{ProtocolVersion: model.AgentReportProtocol, ReportIntervalMS: interval.Milliseconds(),
			Name: config.NodeName, Host: config.AdvertisedHost, Port: config.SSHPort},
		client: &http.Client{Timeout: httpClientTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (r *HTTPReporter) Report(ctx context.Context, sample ReportSample) (key string, resultErr error) {
	defer func() {
		if resultErr != nil && r.failed != nil {
			r.failed(resultErr)
		}
	}()
	report := r.report
	report.SampleID, report.CapturedAt = sample.SampleID, sample.CapturedAt
	report.System, report.Metrics, report.CustomMetrics = sample.Metrics.SystemInfo, &sample.Metrics, sample.CustomMetrics
	report.CustomSamples = sample.CustomSamples
	report.Accelerators = sample.Accelerators
	if err := report.Validate(time.Now()); err != nil {
		return "", invalidLocalReport(err)
	}
	body, err := json.Marshal(report)
	if err != nil || len(body) > model.MaximumAgentReportBytes {
		return "", &ReportError{Code: "invalid_report"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", &ReportError{Code: "request_failed"}
	}
	request.Header.Set("Authorization", "Bearer "+r.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &ReportError{Code: "transport_failed", Retryable: true}
	}
	defer func() { _ = response.Body.Close() }()
	return decodeReportACKWithReceivers(response, report, reportReceivers{policy: r.acceleratorPolicy, accepted: r.accepted})
}

func invalidLocalReport(err error) error {
	code := "invalid_report"
	if errors.Is(err, model.ErrAgentReportExpired) {
		code = "sample_expired"
	}
	if errors.Is(err, model.ErrAgentReportFuture) {
		code = "clock_skew"
	}
	return &ReportError{Code: code}
}

type reportReceivers struct {
	policy   func(model.AcceleratorPolicy) error
	accepted func(time.Time)
}

func (r *HTTPReporter) WithReportObserver(accepted func(time.Time), failed func(error)) {
	r.accepted, r.failed = accepted, failed
}

func decodeReportACKWithReceivers(response *http.Response, report NodeReport, receivers reportReceivers) (string, error) {
	status := response.StatusCode
	retry := status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
	failure := &ReportError{Code: "rejected", Status: status, Retryable: retry,
		RetryAfter: parseReportRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	ack, err := readReportACK(response, failure)
	if err != nil {
		return "", err
	}
	if err = applyReportAcceleratorPolicy(ack, report, receivers.policy); err != nil {
		return "", err
	}
	if !successfulReportStatus(status) {
		failure.Code = safeReportCode(ack.ErrorCode)
		return "", failure
	}
	if !completeReportACK(ack, report) {
		return "", &ReportError{Code: "invalid_ack", Status: status, Retryable: true, RetryAfter: failure.RetryAfter}
	}
	if receivers.accepted != nil {
		receivers.accepted(*ack.ContactAt)
	}
	return strings.TrimSpace(ack.SSHPublicKey), nil
}

func readReportACK(response *http.Response, failure *ReportError) (model.AgentReportACK, error) {
	content, err := io.ReadAll(io.LimitReader(response.Body, maximumReportACKBytes+1))
	var envelope struct {
		Data model.AgentReportACK `json:"data"`
	}
	decodeErr := json.Unmarshal(content, &envelope)
	if err != nil || len(content) > maximumReportACKBytes || decodeErr != nil {
		failure.Code = "invalid_ack"
		failure.Retryable = failure.Retryable || successfulReportStatus(response.StatusCode)
		return model.AgentReportACK{}, failure
	}
	return envelope.Data, nil
}

func applyReportAcceleratorPolicy(ack model.AgentReportACK, report NodeReport, receiver func(model.AcceleratorPolicy) error) error {
	if receiver == nil || ack.AcceleratorPolicy == nil || !ack.Matches(report) {
		return nil
	}
	if err := receiver(*ack.AcceleratorPolicy); err != nil {
		return &ReportError{Code: "invalid_ack", Retryable: true}
	}
	return nil
}

func completeReportACK(ack model.AgentReportACK, report NodeReport) bool {
	return ack.Matches(report) && ack.ContactAccepted && ack.MetricsAccepted && ack.ContactAt != nil && !ack.ContactAt.IsZero() && !ack.Retryable && ack.ErrorCode == ""
}

func successfulReportStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}
