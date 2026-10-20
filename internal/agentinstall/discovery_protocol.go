package agentinstall

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

const (
	// grantNonceBytes 是轮询与认领签名使用的随机数长度。
	grantNonceBytes = 24
	// grantResponseLimit 限制入网响应的读取上限。
	grantResponseLimit = 64 * 1024
)

// 下列结构逐字段对齐服务端入网协议响应，避免依赖服务端内部类型。
type grantChallenge struct {
	ChallengeID     string `json:"challenge_id"`
	Challenge       string `json:"challenge"`
	Origin          string `json:"origin"`
	IssuedAt        string `json:"issued_at"`
	ExpiresAt       string `json:"expires_at"`
	ProtocolVersion string `json:"protocol_version"`
}

type grantRequestCredential struct {
	RequestID    string `json:"request_id"`
	RequestToken string `json:"request_token"`
	Status       string `json:"status"`
	GrantName    string `json:"grant_name"`
}

type grantStatus struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Ticket    string `json:"ticket"`
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
}

type grantClaim struct {
	NodeID         string `json:"node_id"`
	NodeName       string `json:"node_name"`
	GroupID        string `json:"group_id"`
	ServerURL      string `json:"server_url"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	AgentToken     string `json:"agent_token"`
	ReportInterval string `json:"report_interval"`
	Recovered      bool   `json:"recovered"`
}

// grantEnvelope 是服务端统一响应信封。
type grantEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// challengeState 保存一次挑战的绑定材料，直到请求创建完成。
type challengeState struct {
	ID        string
	Challenge []byte
	Origin    string
	ExpiresAt time.Time
}

// discoveryClient 是 Agent 侧的入网协议客户端；Origin 来自服务端挑战响应。
type discoveryClient struct {
	client   *http.Client
	server   string
	origin   string
	grant    string
	identity Identity
	random   io.Reader
	now      func() time.Time
}

func (installer *Installer) newDiscoveryClient(server, grant string, identity Identity) *discoveryClient {
	random := installer.random
	if random == nil {
		random = rand.Reader
	}
	return &discoveryClient{client: installer.client, server: server, grant: grant, identity: identity,
		random: random, now: time.Now}
}

// challenge 申请一次性挑战，并回读服务端规范来源与过期时间。
func (client *discoveryClient) challenge(ctx context.Context, metadata agentenrollment.Metadata) (challengeState, error) {
	digest := agentenrollment.MetadataDigest(metadata)
	body := map[string]string{"public_key": client.publicKey(), "metadata_digest": encodeAgentBytes(digest[:])}
	response, err := client.post(ctx, "/api/v1/agent-enrollment/challenge", client.grant, body)
	if err != nil {
		return challengeState{}, err
	}
	payload, err := decodeGrantEnvelope[grantChallenge](response)
	if err != nil {
		return challengeState{}, err
	}
	challenge, err := base64.RawURLEncoding.DecodeString(payload.Challenge)
	if err != nil || len(challenge) != agentenrollment.ChallengeSize {
		return challengeState{}, errors.New("server returned a malformed enrollment challenge")
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return challengeState{}, errors.New("server returned a malformed challenge expiry")
	}
	if payload.ProtocolVersion != agentenrollment.ProtocolVersion || payload.Origin == "" {
		return challengeState{}, errors.New("server enrollment protocol does not match this agent")
	}
	client.origin = payload.Origin
	return challengeState{ID: payload.ChallengeID, Challenge: challenge, Origin: payload.Origin,
		ExpiresAt: expiresAt.UTC()}, nil
}

// createRequest 提交元数据与签名，换取只在本次响应出现的请求令牌。
func (client *discoveryClient) createRequest(ctx context.Context, state challengeState,
	metadata agentenrollment.Metadata) (grantRequestCredential, error) {
	canonical := metadata.Canonical()
	digest := agentenrollment.Digest(canonical)
	binding := agentenrollment.RequestBinding{ChallengeID: state.ID, Challenge: state.Challenge,
		Origin: state.Origin, Fingerprint: client.identity.Fingerprint(), Metadata: canonical,
		ExpiresAt: state.ExpiresAt}
	body := map[string]any{
		"challenge_id": state.ID, "challenge": encodeAgentBytes(state.Challenge),
		"public_key": client.publicKey(), "metadata_digest": encodeAgentBytes(digest[:]),
		"metadata":  metadataPayload(metadata),
		"signature": encodeAgentBytes(agentenrollment.SignWith(client.identity.PrivateKey, binding.Canonical())),
	}
	response, err := client.post(ctx, "/api/v1/agent-enrollment/requests", client.grant, body)
	if err != nil {
		return grantRequestCredential{}, err
	}
	return decodeGrantEnvelope[grantRequestCredential](response)
}

// status 轮询请求状态；审批通过或仍在恢复窗口内时会带来新的票据。
func (client *discoveryClient) status(ctx context.Context, requestID, token string) (grantStatus, error) {
	nonce, err := client.newNonce()
	if err != nil {
		return grantStatus{}, err
	}
	timestamp := client.now().UTC().Truncate(time.Second)
	binding := agentenrollment.StatusBinding{RequestID: requestID, Origin: client.origin,
		Timestamp: timestamp, Nonce: nonce}
	body := map[string]string{"timestamp": timestamp.Format(time.RFC3339),
		"nonce":     encodeAgentBytes(nonce),
		"signature": encodeAgentBytes(agentenrollment.SignWith(client.identity.PrivateKey, binding.Canonical()))}
	response, err := client.post(ctx, "/api/v1/agent-enrollment/requests/"+requestID+"/status", token, body)
	if err != nil {
		return grantStatus{}, err
	}
	return decodeGrantEnvelope[grantStatus](response)
}

// claim 用票据兑换节点令牌；这是唯一一次交付节点凭据的响应。
func (client *discoveryClient) claim(ctx context.Context, requestID, token, nodeID, ticket string) (grantClaim, error) {
	nonce, err := client.newNonce()
	if err != nil {
		return grantClaim{}, err
	}
	timestamp := client.now().UTC().Truncate(time.Second)
	binding := agentenrollment.ClaimBinding{RequestID: requestID, NodeID: nodeID, Ticket: ticket,
		Origin: client.origin, Timestamp: timestamp, Nonce: nonce}
	body := map[string]string{"ticket": ticket, "timestamp": timestamp.Format(time.RFC3339),
		"nonce":     encodeAgentBytes(nonce),
		"signature": encodeAgentBytes(agentenrollment.SignWith(client.identity.PrivateKey, binding.Canonical()))}
	response, err := client.post(ctx, "/api/v1/agent-enrollment/requests/"+requestID+"/claim", token, body)
	if err != nil {
		return grantClaim{}, err
	}
	return decodeGrantEnvelope[grantClaim](response)
}

func (client *discoveryClient) publicKey() string {
	return encodeAgentBytes(client.identity.PublicKey)
}

func (client *discoveryClient) newNonce() ([]byte, error) {
	nonce := make([]byte, grantNonceBytes)
	if _, err := io.ReadFull(client.random, nonce); err != nil {
		return nil, fmt.Errorf("generate enrollment nonce: %w", err)
	}
	return nonce, nil
}

// post 发送一次签名或授权请求；失败时只回传服务端给出的稳定文案。
func (client *discoveryClient) post(ctx context.Context, path, bearer string, body any) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode enrollment request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.server+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create enrollment request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+bearer)
	response, err := client.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("enrollment request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, grantResponseError(response)
	}
	return response, nil
}

// grantResponseError 把服务端错误翻译成可操作但不泄露细节的提示。
func grantResponseError(response *http.Response) error {
	var envelope grantEnvelope[json.RawMessage]
	if err := json.NewDecoder(io.LimitReader(response.Body, grantResponseLimit)).Decode(&envelope); err == nil && envelope.Message != "" {
		return fmt.Errorf("server rejected enrollment (%d): %s", response.StatusCode, envelope.Message)
	}
	return fmt.Errorf("server rejected enrollment with status %d", response.StatusCode)
}

func decodeGrantEnvelope[T any](response *http.Response) (T, error) {
	var target T
	envelope := grantEnvelope[T]{}
	if err := json.NewDecoder(io.LimitReader(response.Body, grantResponseLimit)).Decode(&envelope); err != nil {
		return target, errors.New("invalid enrollment response: expected a bounded JSON envelope")
	}
	if envelope.Code < 200 || envelope.Code > 299 {
		return target, errors.New("server rejected enrollment operation")
	}
	return envelope.Data, nil
}

func encodeAgentBytes(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }

// metadataPayload 是 Agent 上报的环境信息，字段名与服务端请求体一致。
func metadataPayload(metadata agentenrollment.Metadata) map[string]any {
	return map[string]any{"hostname": metadata.Hostname, "advertised_host": metadata.AdvertisedHost,
		"os": metadata.OS, "architecture": metadata.Architecture, "agent_version": metadata.AgentVersion,
		"virtualization": metadata.Virtualization, "ssh_port": metadata.SSHPort,
		"installation_nonce": metadata.InstallationNonce}
}
