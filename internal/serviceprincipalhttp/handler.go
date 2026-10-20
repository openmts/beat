package serviceprincipalhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/serviceprincipal"
	"github.com/beat/backend/internal/store"
)

// MaximumJSONBytes 是管理请求体上限，超限直接拒绝。
const MaximumJSONBytes = 64 << 10

const maximumJSONBytes = MaximumJSONBytes

var (
	// ErrInvalid 表示请求体或输入不符合管理契约。
	ErrInvalid = errors.New("service token request is invalid")
	// ErrForbidden 表示调用方没有服务令牌管理身份。
	ErrForbidden = errors.New("service token administration is not permitted")
)

// Handler 暴露服务令牌的 owner-only 管理接口。
type Handler struct{ service *serviceprincipal.Service }

// New 创建管理处理器。
func New(service *serviceprincipal.Service) *Handler { return &Handler{service: service} }

func headers(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func respond(w http.ResponseWriter, value any, err error) {
	headers(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	status, message := http.StatusOK, "success"
	if err != nil {
		status, message, value = errorStatus(err), "service token request was rejected", nil
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}{status, message, value})
}

func errorStatus(err error) int {
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, serviceprincipal.ErrName), errors.Is(err, serviceprincipal.ErrPurpose),
		errors.Is(err, serviceprincipal.ErrScope), errors.Is(err, serviceprincipal.ErrResource),
		errors.Is(err, serviceprincipal.ErrSource), errors.Is(err, serviceprincipal.ErrExpiry):
		return http.StatusBadRequest
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, serviceprincipal.ErrPrincipalNotFound):
		return http.StatusNotFound
	case errors.Is(err, serviceprincipal.ErrPrincipalRevoked), errors.Is(err, serviceprincipal.ErrRotationConflict):
		return http.StatusConflict
	case errors.Is(err, serviceprincipal.ErrStorage):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func decode(r *http.Request, value any) error {
	if encoding := r.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maximumJSONBytes+1))
	if err != nil || len(body) > maximumJSONBytes {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func actor(r *http.Request) (store.ServicePrincipalActor, error) {
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok {
		return store.ServicePrincipalActor{}, ErrForbidden
	}
	return store.ServicePrincipalActor{UserID: principal.User.ID, SessionID: principal.Session.ID,
		RequestID: middleware.RequestID(r.Context()), IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()}, nil
}

func (h *Handler) admin(w http.ResponseWriter, r *http.Request, action func(store.ServicePrincipalActor) (any, error)) {
	principal, err := actor(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	value, err := action(principal)
	respond(w, value, err)
}
