package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// MaxJSONBodyBytes 是管理面与认证面 JSON 请求体的默认预算。它只限制解码期占用，
// 不替代字段级校验：实体数量、字符串长度等语义上限仍由各处理器自行强制。
const MaxJSONBodyBytes = 1 << 20

// BodyErrorReason 是解码失败原因的有界枚举；对外只暴露稳定状态码与固定文案。
type BodyErrorReason string

const (
	BodyReasonMalformed BodyErrorReason = "malformed"
	BodyReasonOversize  BodyErrorReason = "oversize"
	BodyReasonMediaType BodyErrorReason = "media_type"
	BodyReasonCancelled BodyErrorReason = "cancelled"
	BodyReasonTrailing  BodyErrorReason = "trailing"
	BodyReasonInternal  BodyErrorReason = "internal"
)

// BodyError 把解码失败归类为稳定原因，不携带解码器内部信息。
type BodyError struct {
	Reason BodyErrorReason
	cause  error
}

func (e *BodyError) Error() string { return "request body " + string(e.Reason) }

// Unwrap 保留底层错误（例如取消），供调用方用 errors.Is 判断。
func (e *BodyError) Unwrap() error { return e.cause }

// Status 是原因对应的稳定 HTTP 状态码。
func (e *BodyError) Status() int {
	switch e.Reason {
	case BodyReasonOversize:
		return http.StatusRequestEntityTooLarge
	case BodyReasonMediaType:
		return http.StatusUnsupportedMediaType
	case BodyReasonCancelled:
		return http.StatusRequestTimeout
	default:
		return http.StatusBadRequest
	}
}

// Message 是不泄露解码器内部的固定文案。
func (e *BodyError) Message() string {
	switch e.Reason {
	case BodyReasonOversize:
		return "request body too large"
	case BodyReasonMediaType:
		return "unsupported media type"
	case BodyReasonCancelled:
		return "request cancelled"
	default:
		return "invalid request body"
	}
}

// WriteBodyError 写出稳定错误响应；非 BodyError 统一按非法请求处理。
func WriteBodyError(w http.ResponseWriter, err error) {
	var bodyErr *BodyError
	if !errors.As(err, &bodyErr) {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	JSONError(w, bodyErr.Status(), bodyErr.Message())
}

// DecodeJSONBody 在预算内读取请求体，校验媒体类型，并只接受恰好一个 JSON 值。
// strict 为真时拒绝未知字段，用于管理面与认证面载荷。
func DecodeJSONBody(r *http.Request, target interface{}, limit int64, strict bool) error {
	if err := r.Context().Err(); err != nil {
		return &BodyError{Reason: BodyReasonCancelled, cause: err}
	}
	if limit <= 0 {
		limit = MaxJSONBodyBytes
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return readBodyError(r.Context(), err)
	}
	if int64(len(raw)) > limit {
		return &BodyError{Reason: BodyReasonOversize}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		// 空请求体保留原有语义：调用方决定是否容忍 io.EOF。
		return io.EOF
	}
	if err := requireJSONMediaType(r.Header.Get("Content-Type")); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return decodeError(r.Context(), err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &BodyError{Reason: BodyReasonTrailing}
	}
	return nil
}

func requireJSONMediaType(raw string) error {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err != nil || mediaType != "application/json" {
		return &BodyError{Reason: BodyReasonMediaType}
	}
	return nil
}

func readBodyError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &BodyError{Reason: BodyReasonCancelled, cause: err}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &BodyError{Reason: BodyReasonOversize, cause: err}
	}
	return &BodyError{Reason: BodyReasonInternal, cause: err}
}

func decodeError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &BodyError{Reason: BodyReasonCancelled, cause: err}
	}
	return &BodyError{Reason: BodyReasonMalformed, cause: err}
}

// ParseJSON 读取非严格 JSON 载荷：有界、媒体类型受检、只接受一个 JSON 值。
func ParseJSON(r *http.Request, v interface{}) error {
	return DecodeJSONBody(r, v, MaxJSONBodyBytes, false)
}

// ParseJSONStrict 用于管理面与认证面：额外拒绝未知字段。
func ParseJSONStrict(r *http.Request, v interface{}) error {
	return DecodeJSONBody(r, v, MaxJSONBodyBytes, true)
}
