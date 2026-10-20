package agentdiagnostics

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

	"github.com/beat/backend/internal/diagnosticprotocol"
)

const maximumResponseBytes = 256 << 10

const requestTimeout = 10 * time.Second

// Gateway 是 Agent 依赖的服务端诊断接口。
type Gateway interface {
	Advertise(context.Context, diagnosticprotocol.CapabilityDocument) (string, error)
	Lease(context.Context, string) (*diagnosticprotocol.Lease, error)
	Start(context.Context, diagnosticprotocol.AttemptRequest) error
	Heartbeat(context.Context, diagnosticprotocol.AttemptRequest) (bool, error)
	Progress(context.Context, diagnosticprotocol.ProgressRequest) (int64, error)
	Complete(context.Context, diagnosticprotocol.CompletionRequest) error
}

// Client 是诊断协议的 HTTP 实现；重定向与压缩都被拒绝。
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
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" ||
		(address.Scheme != "http" && address.Scheme != "https") || config.Token == "" {
		return nil, ErrTransport
	}
	client := &http.Client{Timeout: requestTimeout, Transport: config.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{base: strings.TrimRight(config.ServerURL, "/"), token: config.Token, http: client}, nil
}

func (client *Client) Advertise(ctx context.Context, document diagnosticprotocol.CapabilityDocument) (string, error) {
	var response diagnosticprotocol.AdvertiseResponse
	if err := client.callInto(ctx, "capability", document, &response); err != nil {
		return "", err
	}
	if len(response.Digest) != 64 {
		return "", ErrTransport
	}
	return response.Digest, nil
}

func (client *Client) Lease(ctx context.Context, digest string) (*diagnosticprotocol.Lease, error) {
	wire := struct {
		CapabilityDigest string `json:"capability_digest"`
	}{CapabilityDigest: digest}
	var lease diagnosticprotocol.Lease
	if err := client.callInto(ctx, "lease", wire, &lease); err != nil {
		if errors.Is(err, ErrPolicy) {
			return nil, nil
		}
		return nil, err
	}
	if lease.JobID == "" || lease.Attempt < 1 || lease.Nonce == "" {
		return nil, nil
	}
	return &lease, nil
}

func (client *Client) Start(ctx context.Context, attempt diagnosticprotocol.AttemptRequest) error {
	var response diagnosticprotocol.Acknowledgement
	if err := client.callInto(ctx, "attempts/"+attempt.JobID+"/start", attempt, &response); err != nil {
		return err
	}
	if !response.Acknowledged {
		return ErrTransport
	}
	return nil
}

func (client *Client) Heartbeat(ctx context.Context, attempt diagnosticprotocol.AttemptRequest) (bool, error) {
	var response diagnosticprotocol.HeartbeatResponse
	if err := client.callInto(ctx, "attempts/"+attempt.JobID+"/heartbeat", attempt, &response); err != nil {
		return false, err
	}
	return response.CancelRequested, nil
}

func (client *Client) Progress(ctx context.Context, frame diagnosticprotocol.ProgressRequest) (int64, error) {
	var response diagnosticprotocol.ProgressResponse
	if err := client.callInto(ctx, "attempts/"+frame.JobID+"/progress", frame, &response); err != nil {
		return 0, err
	}
	return response.Sequence, nil
}

func (client *Client) Complete(ctx context.Context, completion diagnosticprotocol.CompletionRequest) error {
	var response diagnosticprotocol.Acknowledgement
	if err := client.callInto(ctx, "attempts/"+completion.JobID+"/complete", completion, &response); err != nil {
		return err
	}
	if !response.Acknowledged {
		return ErrTransport
	}
	return nil
}

func (client *Client) callInto(ctx context.Context, path string, input, output any) error {
	content, err := json.Marshal(input)
	if err != nil || len(content) > maximumResponseBytes {
		return ErrTransport
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.base+"/api/v1/agent-diagnostics/"+path,
		bytes.NewReader(content))
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
	if err := diagnosticStatusError(response); err != nil {
		return err
	}
	return decodeDiagnosticResponse(response, output)
}

func diagnosticStatusError(response *http.Response) error {
	switch {
	case response.StatusCode == http.StatusOK:
		return nil
	case response.StatusCode >= 400 && response.StatusCode < 500:
		return ErrPolicy
	default:
		return ErrTransport
	}
}

func decodeDiagnosticResponse(response *http.Response, output any) error {
	if response.Header.Get("Content-Encoding") != "" {
		return ErrTransport
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(data) > maximumResponseBytes {
		return ErrTransport
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Code != http.StatusOK {
		return ErrTransport
	}
	if output == nil || len(envelope.Data) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return ErrTransport
	}
	return nil
}
