package handler

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

// agentDiscoveryChallengeTTL 与设计文档一致：挑战两分钟后失效，必须重新申请。
const agentDiscoveryChallengeTTL = 2 * time.Minute

const (
	// discoveryMinimumNonceBytes 阻止 Agent 省略随机数导致签名载荷可预测。
	discoveryMinimumNonceBytes = 16
	// discoveryMaximumNonceBytes 限制签名载荷大小，保持请求体预算可控。
	discoveryMaximumNonceBytes = 64
)

// parseDiscoveryJSON 解码协议请求体：限定媒体类型、体积与未知字段。
func parseDiscoveryJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		JSONError(w, http.StatusUnsupportedMediaType, "application/json is required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, agentDiscoveryMaxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		discoveryJSONError(w, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		discoveryJSONError(w, err)
		return false
	}
	return true
}

func discoveryJSONError(w http.ResponseWriter, err error) {
	var maximum *http.MaxBytesError
	if errors.As(err, &maximum) {
		JSONError(w, http.StatusRequestEntityTooLarge, "enrollment request is too large")
		return
	}
	JSONError(w, http.StatusBadRequest, "invalid enrollment JSON request")
}

// decodeDiscoverySecret 校验明文秘密的前缀与编码，得到可直接比对的落库形态。
func decodeDiscoverySecret(value, prefix string) (agentenrollment.Secret, bool) {
	secret, err := agentenrollment.DescribeSecret(prefix, strings.TrimSpace(value))
	if err != nil {
		return agentenrollment.Secret{}, false
	}
	return secret, true
}

func decodeDiscoveryPublicKey(value string) ([]byte, bool) {
	return decodeDiscoveryFixed(value, ed25519.PublicKeySize)
}

func decodeDiscoverySignature(value string) ([]byte, bool) {
	return decodeDiscoveryFixed(value, ed25519.SignatureSize)
}

func decodeDiscoveryDigest(value string) ([]byte, bool) {
	return decodeDiscoveryFixed(value, sha256.Size)
}

// decodeDiscoveryNonce 要求随机数落在显式大小窗口内。
func decodeDiscoveryNonce(value string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) < discoveryMinimumNonceBytes || len(decoded) > discoveryMaximumNonceBytes {
		return nil, false
	}
	return decoded, true
}

func decodeDiscoveryFixed(value string, size int) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != size {
		return nil, false
	}
	return decoded, true
}

func encodeDiscoveryBytes(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func formatDiscoveryInstant(instant time.Time) string {
	return instant.UTC().Format(time.RFC3339)
}

// parseDiscoveryInstant 只接受 RFC3339 时间戳，供签名载荷逐字节复现。
func parseDiscoveryInstant(value string) (time.Time, bool) {
	instant, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return instant.UTC(), true
}

// withinDiscoverySkew 判断时间戳是否落在允许的时钟偏移窗口内。
func withinDiscoverySkew(instant, now time.Time) bool {
	delta := now.UTC().Sub(instant)
	return delta <= discoveryClockSkew && delta >= -discoveryClockSkew
}

// discoveryReject 记录一次被拒绝的协议调用并返回统一的模糊失败响应。
func discoveryReject(w http.ResponseWriter, handler *AgentDiscoveryHandler, r *http.Request) {
	if address, ok := discoveryClientAddress(r); ok {
		handler.budgets.rejected.acquire(address.String(), handler.now())
	}
	JSONError(w, http.StatusForbidden, "enrollment is not available")
}

// errorsIsStoreBusy 区分容量类拒绝：这些情况重试有意义，应当返回 429。
func errorsIsStoreBusy(err error) bool {
	return errors.Is(err, store.ErrEnrollmentChallengeLimit)
}

// isDiscoveryClientError 把可归因于调用方的失败映射为 400，而不是服务端故障。
func isDiscoveryClientError(err error) bool {
	candidates := []error{
		agentenrollment.ErrInvalidMetadata,
		agentenrollment.ErrInvalidSignature,
		agentenrollment.ErrInvalidGrantPolicy,
		store.ErrEnrollmentChallengeUnavailable,
		store.ErrEnrollmentRequestUnavailable,
		store.ErrEnrollmentRequestCapacity,
		store.ErrEnrollmentTicketUnavailable,
		store.ErrEnrollmentRecoveryUnavailable,
		store.ErrEnrollmentGrantUnavailable,
		store.ErrEnrollmentGrantCapacity,
	}
	for _, candidate := range candidates {
		if errors.Is(err, candidate) {
			return true
		}
	}
	return false
}
