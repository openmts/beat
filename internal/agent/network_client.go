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
	networkAssignmentsPath = "/api/v1/network/assignments"
	networkResultsPath     = "/api/v1/network/results"
	maxNetworkResponseBody = 256 * 1024
)

type NetworkAssignmentSet struct {
	ExpiresAt time.Time
	Tasks     []model.NetworkAssignment
}

type NetworkAssignmentClient interface {
	FetchAssignments(context.Context) (NetworkAssignmentSet, error)
	ReportResults(context.Context, []model.NetworkProbeResult) error
}

type NetworkHTTPClient struct {
	assignmentsURL string
	resultsURL     string
	token          string
	client         *http.Client
}

func NewNetworkHTTPClient(config Config) (*NetworkHTTPClient, error) {
	base, err := url.Parse(config.ServerURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("parse network server URL: %w", err)
	}
	assignments := *base
	assignments.Path = strings.TrimRight(base.Path, "/") + networkAssignmentsPath
	assignments.RawQuery = ""
	results := *base
	results.Path = strings.TrimRight(base.Path, "/") + networkResultsPath
	results.RawQuery = ""
	return &NetworkHTTPClient{
		assignmentsURL: assignments.String(), resultsURL: results.String(),
		token: config.AgentToken, client: &http.Client{Timeout: httpClientTimeout},
	}, nil
}

func (client *NetworkHTTPClient) FetchAssignments(ctx context.Context) (NetworkAssignmentSet, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.assignmentsURL, nil)
	if err != nil {
		return NetworkAssignmentSet{}, fmt.Errorf("create assignment request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	response, err := client.client.Do(request)
	if err != nil {
		return NetworkAssignmentSet{}, fmt.Errorf("fetch network assignments: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return NetworkAssignmentSet{}, networkResponseError(response, "network assignments")
	}
	var envelope struct {
		Data struct {
			ExpiresAt time.Time                 `json:"expires_at"`
			Tasks     []model.NetworkAssignment `json:"tasks"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxNetworkResponseBody))
	if err := decoder.Decode(&envelope); err != nil {
		return NetworkAssignmentSet{}, fmt.Errorf("decode network assignments: %w", err)
	}
	return NetworkAssignmentSet{ExpiresAt: envelope.Data.ExpiresAt, Tasks: envelope.Data.Tasks}, nil
}

func (client *NetworkHTTPClient) ReportResults(ctx context.Context, results []model.NetworkProbeResult) error {
	body, err := json.Marshal(struct {
		Results []model.NetworkProbeResult `json:"results"`
	}{Results: results})
	if err != nil {
		return fmt.Errorf("encode network results: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.resultsURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create network result request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf("report network results: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		return networkResponseError(response, "network results")
	}
	return nil
}

func networkResponseError(response *http.Response, operation string) error {
	message, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	if err != nil {
		return fmt.Errorf("server rejected %s with status %d: %w", operation, response.StatusCode, err)
	}
	return fmt.Errorf("server rejected %s with status %d: %s",
		operation, response.StatusCode, strings.TrimSpace(string(message)))
}
