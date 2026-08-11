package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const webSocketTokenPrefix = "beat-admin-token."

const agentLastUsedInterval = 5 * time.Minute

type webSocketProtocolKey struct{}

type agentIdentityKey struct{}

type AgentAuthenticator interface {
	AuthenticateAgentToken(context.Context, string) (*model.Node, error)
	AuthenticateLegacyNode(context.Context, string) (*model.Node, error)
	TouchAgentToken(context.Context, string, time.Time, time.Duration) error
}

type LegacyNodeNameResolver func(*http.Request) (string, bool)

func BearerAuth(expectedToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expectedToken == "" {
				WriteAuthError(w, http.StatusServiceUnavailable, "authentication is not configured")
				return
			}

			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok || !tokensEqual(token, expectedToken) {
				WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func AgentAuth(
	authenticator AgentAuthenticator,
	legacyToken string,
	legacyNodeName LegacyNodeNameResolver,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			identity, status := authenticateAgent(r, authenticator, legacyToken, legacyNodeName, token)
			if status != http.StatusOK {
				message := "unauthorized"
				if status == http.StatusInternalServerError {
					message = "authentication failed"
				}
				WriteAuthError(w, status, message)
				return
			}
			ctx := context.WithValue(r.Context(), agentIdentityKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AgentIdentity(ctx context.Context) (model.AgentIdentity, bool) {
	identity, ok := ctx.Value(agentIdentityKey{}).(model.AgentIdentity)
	return identity, ok
}

func WithAgentIdentity(ctx context.Context, identity model.AgentIdentity) context.Context {
	return context.WithValue(ctx, agentIdentityKey{}, identity)
}

func authenticateAgent(
	r *http.Request,
	authenticator AgentAuthenticator,
	legacyToken string,
	legacyNodeName LegacyNodeNameResolver,
	token string,
) (model.AgentIdentity, int) {
	node, err := authenticator.AuthenticateAgentToken(r.Context(), token)
	if err != nil {
		return model.AgentIdentity{}, http.StatusInternalServerError
	}
	if node != nil {
		if err := authenticator.TouchAgentToken(
			r.Context(), node.ID, time.Now().UTC(), agentLastUsedInterval,
		); err != nil {
			return model.AgentIdentity{}, http.StatusInternalServerError
		}
		return nodeIdentity(*node, model.AgentCredentialActive), http.StatusOK
	}
	if legacyNodeName == nil || legacyToken == "" || !tokensEqual(token, legacyToken) {
		return model.AgentIdentity{}, http.StatusUnauthorized
	}
	name, ok := legacyNodeName(r)
	if !ok || name == "" {
		return model.AgentIdentity{}, http.StatusUnauthorized
	}
	node, err = authenticator.AuthenticateLegacyNode(r.Context(), name)
	if err != nil {
		return model.AgentIdentity{}, http.StatusInternalServerError
	}
	if node == nil {
		return model.AgentIdentity{}, http.StatusUnauthorized
	}
	return nodeIdentity(*node, model.AgentCredentialLegacy), http.StatusOK
}

func nodeIdentity(node model.Node, mode string) model.AgentIdentity {
	return model.AgentIdentity{NodeID: node.ID, NodeName: node.Name, Mode: mode}
}

func WebSocketBearerAuth(expectedToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expectedToken == "" {
				WriteAuthError(w, http.StatusServiceUnavailable, "authentication is not configured")
				return
			}

			protocol, token, ok := webSocketToken(r.Header.Get("Sec-WebSocket-Protocol"))
			if !ok || !tokensEqual(token, expectedToken) {
				WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			ctx := context.WithValue(r.Context(), webSocketProtocolKey{}, protocol)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func WebSocketTokenProtocol(token string) string {
	return webSocketTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(token))
}

func SelectedWebSocketProtocol(r *http.Request) string {
	protocol, _ := r.Context().Value(webSocketProtocolKey{}).(string)
	return protocol
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != "" && !strings.Contains(token, " ")
}

func webSocketToken(header string) (string, string, bool) {
	for protocol := range strings.SplitSeq(header, ",") {
		protocol = strings.TrimSpace(protocol)
		if !strings.HasPrefix(protocol, webSocketTokenPrefix) {
			continue
		}

		encoded := strings.TrimPrefix(protocol, webSocketTokenPrefix)
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(decoded) == 0 {
			return "", "", false
		}
		return protocol, string(decoded), true
	}

	return "", "", false
}

func tokensEqual(actual, expected string) bool {
	actualHash := sha256.Sum256([]byte(actual))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(actualHash[:], expectedHash[:]) == 1
}

func WriteAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{Code: status, Message: message})
}
