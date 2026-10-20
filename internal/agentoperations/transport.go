package agentoperations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

var ErrTransport = errors.New("operation protocol request was not acknowledged")

var ErrLeaseRejected = errors.New("operation lease is no longer authorized")

type Gateway interface {
	Lease(context.Context, protocol.Policy) (*protocol.Lease, error)
	Start(context.Context, protocol.Lease) error
	Heartbeat(context.Context, protocol.Lease) (bool, error)
	Complete(context.Context, protocol.Completion) error
}

type Client struct {
	base, token string
	http        *http.Client
}

type ClientConfig struct {
	ServerURL, Token string
	Transport        http.RoundTripper
}

func NewClient(config ClientConfig) (*Client, error) {
	address, err := url.Parse(config.ServerURL)
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || (address.Scheme != "http" && address.Scheme != "https") || config.Token == "" {
		return nil, ErrTransport
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: config.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{base: strings.TrimRight(config.ServerURL, "/"), token: config.Token, http: client}, nil
}

func CapabilityDigest(policy protocol.Policy) (string, error) {
	if err := policy.Validate(); err != nil {
		return "", ErrPolicy
	}
	content, err := json.Marshal(policy)
	if err != nil {
		return "", ErrPolicy
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}

func (client *Client) Lease(ctx context.Context, policy protocol.Policy) (*protocol.Lease, error) {
	digest, err := CapabilityDigest(policy)
	if err != nil {
		return nil, err
	}
	var response protocol.AgentResponse[*protocol.Lease]
	err = client.request(ctx, wireRequest{path: "lease", input: protocol.AgentLeaseInput{CapabilityDigest: digest, Capabilities: policy}, output: &response})
	return response.Data, err
}

func (client *Client) Start(ctx context.Context, lease protocol.Lease) error {
	var response protocol.AgentResponse[protocol.StartAuthorization]
	if err := client.attempt(ctx, lease, wireRequest{path: "start", output: &response}); err != nil {
		return err
	}
	if !response.Data.Authorized {
		return ErrTransport
	}
	return nil
}

func (client *Client) Heartbeat(ctx context.Context, lease protocol.Lease) (bool, error) {
	var response protocol.AgentResponse[protocol.HeartbeatResponse]
	err := client.attempt(ctx, lease, wireRequest{path: "heartbeat", output: &response})
	return response.Data.CancelRequested, err
}

func (client *Client) Complete(ctx context.Context, completion protocol.Completion) error {
	var response protocol.AgentResponse[protocol.CompletionReceipt]
	input := protocol.AgentCompletionInput{Nonce: completion.Attempt.Nonce, Result: completion.Result}
	if err := client.attempt(ctx, protocol.Lease{AttemptID: completion.Attempt.ID, Nonce: completion.Attempt.Nonce}, wireRequest{path: "complete", input: input, output: &response}); err != nil {
		return err
	}
	if !response.Data.Acknowledged {
		return ErrTransport
	}
	return nil
}

type wireRequest struct {
	path          string
	input, output any
}

func (client *Client) attempt(ctx context.Context, lease protocol.Lease, request wireRequest) error {
	if !validAttemptID(lease.AttemptID) {
		return ErrTransport
	}
	if request.input == nil {
		request.input = protocol.AgentAttemptInput{Nonce: lease.Nonce}
	}
	request.path = "attempts/" + lease.AttemptID + "/" + request.path
	return client.request(ctx, request)
}

func (client *Client) request(ctx context.Context, wire wireRequest) error {
	content, err := json.Marshal(wire.input)
	if err != nil {
		return ErrTransport
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.base+"/api/v1/agent-operations/"+wire.path, bytes.NewReader(content))
	if err != nil {
		return ErrTransport
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return ErrTransport
	}
	defer func() { _ = response.Body.Close() }()
	if err := wireStatusError(response, wire.path); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumJournalBytes+1))
	if err != nil || len(data) > maximumJournalBytes {
		return ErrTransport
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Code != http.StatusOK {
		return ErrTransport
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(wire.output); err != nil {
		return ErrTransport
	}
	return nil
}

func validAttemptID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func wireStatusError(response *http.Response, path string) error {
	if response.StatusCode == http.StatusOK {
		return nil
	}
	if strings.HasSuffix(path, "/chunks") {
		return fileStatusError(response.StatusCode, response.Header)
	}
	if strings.HasSuffix(path, "/start") && (response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusNotFound) {
		return ErrLeaseRejected
	}
	return ErrTransport
}

func fileStatusError(status int, header http.Header) error {
	if status == http.StatusConflict && header.Get("Retry-After") != "" {
		return ErrTransport
	}
	if status == http.StatusConflict || status == http.StatusNotFound {
		return ErrFileConflict
	}
	if status >= 400 && status < 500 {
		return ErrPolicy
	}
	return ErrTransport
}
