package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	reportPath        = "/api/v1/nodes/report"
	httpClientTimeout = 15 * time.Second
	maxErrorBody      = 4096
)

type NodeReport struct {
	Name    string            `json:"name"`
	Host    string            `json:"host"`
	Port    int               `json:"port"`
	System  model.SystemInfo  `json:"system"`
	Metrics model.NodeMetrics `json:"metrics"`
}

type HTTPReporter struct {
	endpoint string
	token    string
	report   NodeReport
	client   *http.Client
}

func NewHTTPReporter(config Config) (*HTTPReporter, error) {
	serverURL, err := url.Parse(config.ServerURL)
	if err != nil || (serverURL.Scheme != "http" && serverURL.Scheme != "https") || serverURL.Host == "" {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	serverURL.Path = strings.TrimRight(serverURL.Path, "/") + reportPath
	serverURL.RawQuery = ""
	serverURL.Fragment = ""
	return &HTTPReporter{
		endpoint: serverURL.String(), token: config.AgentToken,
		report: NodeReport{Name: config.NodeName, Host: config.AdvertisedHost, Port: config.SSHPort},
		client: &http.Client{Timeout: httpClientTimeout},
	}, nil
}

func (r *HTTPReporter) Report(ctx context.Context, metrics model.NodeMetrics) error {
	report := r.report
	report.System = metrics.SystemInfo
	report.Metrics = metrics
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode node report: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create report request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+r.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("send node report: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		if readErr != nil {
			return fmt.Errorf("server rejected node report with status %d and unreadable body: %w", response.StatusCode, readErr)
		}
		return fmt.Errorf("server rejected node report with status %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("read node report response: %w", err)
	}
	return nil
}
