package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeBodyRequest(t *testing.T, contentType, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request
}

func TestDecodeJSONBodyReasonsAndStatuses(t *testing.T) {
	var target struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name        string
		contentType string
		body        string
		strict      bool
		reason      BodyErrorReason
		status      int
	}{
		{name: "valid", contentType: "application/json; charset=utf-8", body: `{"name":"a"}`},
		{name: "malformed", contentType: "application/json", body: `{"name":`, reason: BodyReasonMalformed, status: http.StatusBadRequest},
		{name: "wrong media type", contentType: "text/plain", body: `{"name":"a"}`, reason: BodyReasonMediaType, status: http.StatusUnsupportedMediaType},
		{name: "missing media type", body: `{"name":"a"}`, reason: BodyReasonMediaType, status: http.StatusUnsupportedMediaType},
		{name: "trailing value", contentType: "application/json", body: `{"name":"a"}{"name":"b"}`, reason: BodyReasonTrailing, status: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"extra":1}`, strict: true, reason: BodyReasonMalformed, status: http.StatusBadRequest},
		{name: "unknown field tolerated", contentType: "application/json", body: `{"name":"a","extra":1}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := decodeBodyRequest(t, test.contentType, test.body)
			err := DecodeJSONBody(request, &target, 0, test.strict)
			if test.reason == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var bodyErr *BodyError
			if !errors.As(err, &bodyErr) || bodyErr.Reason != test.reason {
				t.Fatalf("error = %v, want reason %q", err, test.reason)
			}
			if bodyErr.Status() != test.status {
				t.Fatalf("status = %d, want %d", bodyErr.Status(), test.status)
			}
			recorder := httptest.NewRecorder()
			WriteBodyError(recorder, err)
			if recorder.Code != test.status {
				t.Fatalf("written status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestDecodeJSONBodyBoundsAndCancellation(t *testing.T) {
	target := map[string]any{}
	oversize := decodeBodyRequest(t, "application/json", `{"name":"`+strings.Repeat("x", 2048)+`"}`)
	err := DecodeJSONBody(oversize, &target, 64, false)
	var bodyErr *BodyError
	if !errors.As(err, &bodyErr) || bodyErr.Reason != BodyReasonOversize {
		t.Fatalf("oversize error = %v", err)
	}
	if bodyErr.Status() != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status = %d", bodyErr.Status())
	}

	empty := decodeBodyRequest(t, "", "")
	if err := DecodeJSONBody(empty, &target, 0, false); !errors.Is(err, io.EOF) {
		t.Fatalf("empty body error = %v, want io.EOF", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	request := decodeBodyRequest(t, "application/json", `{"name":"a"}`)
	if err := DecodeJSONBody(request.WithContext(cancelled), &target, 0, false); !errors.As(err, &bodyErr) ||
		bodyErr.Reason != BodyReasonCancelled {
		t.Fatalf("cancelled error = %v", err)
	}
}
