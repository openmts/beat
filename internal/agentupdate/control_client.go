package agentupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const leasePath = "/api/v1/agents/rollout/lease"

const observationPath = "/api/v1/agents/rollout/observations"

type ControlClient struct{ fetcher *HTTPFetcher }

func NewControlClient(config HTTPConfig) (*ControlClient, error) {
	fetcher, err := NewHTTPFetcher(config)
	if err != nil {
		return nil, err
	}
	return &ControlClient{fetcher: fetcher}, nil
}

func (client *ControlClient) Close() { client.fetcher.Close() }

func (client *ControlClient) Lease(ctx context.Context, caps protocol.Capabilities) (*protocol.Assignment, error) {
	data, err := client.post(ctx, leasePath, caps)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	var assignment protocol.Assignment
	if err = decodePrivateJSON(data, &assignment); err != nil {
		return nil, ErrDownload
	}
	return &assignment, nil
}

func (client *ControlClient) Observe(ctx context.Context, observation protocol.Observation) error {
	_, err := client.post(ctx, observationPath, observation)
	return err
}

func (client *ControlClient) post(ctx context.Context, path string, body any) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, ErrAssignment
	}
	bounded, cancel := context.WithTimeout(ctx, transferRequestTimeout)
	defer cancel()
	endpoint := *client.fetcher.server
	endpoint.Path += path
	request, err := http.NewRequestWithContext(bounded, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, ErrDownload
	}
	request.Header.Set("Authorization", "Bearer "+client.fetcher.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.fetcher.client.Do(request)
	if err != nil {
		return nil, ErrDownload
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, ErrDownload
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumJournalBytes+1))
	if err != nil || len(data) > maximumJournalBytes {
		return nil, ErrDownload
	}
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err = decodePrivateJSON(data, &envelope); err != nil || envelope.Code == nil || *envelope.Code != http.StatusOK {
		return nil, ErrDownload
	}
	return envelope.Data, nil
}
