package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/model"
)

type fakePublicTelemetryPolicy struct {
	policy model.PublicTelemetryPolicy
	err    error
}

func (fake *fakePublicTelemetryPolicy) Get(context.Context) (model.PublicTelemetryPolicy, error) {
	return fake.policy, fake.err
}

type fakePublicInteractionWriter struct {
	accepted   []model.PublicInteraction
	rejections []string
	err        error
}

func (fake *fakePublicInteractionWriter) WritePublicInteractions(_ context.Context, _ time.Time, interactions []model.PublicInteraction) error {
	if fake.err != nil {
		return fake.err
	}
	fake.accepted = append(fake.accepted, interactions...)
	return nil
}

func (fake *fakePublicInteractionWriter) WritePublicInteractionRejections(_ context.Context, _ time.Time, reasons []string) error {
	if fake.err != nil {
		return fake.err
	}
	fake.rejections = append(fake.rejections, reasons...)
	return nil
}

func enabledPolicy() model.PublicTelemetryPolicy {
	policy := model.DefaultPublicTelemetryPolicy()
	policy.Enabled = true
	return policy
}

func wildcardOrigins(t *testing.T) deploymentaccess.OriginPolicy {
	t.Helper()
	policy, err := deploymentaccess.ParseOrigins("*")
	if err != nil {
		t.Fatalf("parse origins: %v", err)
	}
	return policy
}

func interactionRequest(t *testing.T, body, contentType string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/public/interactions", strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	request.RemoteAddr = "203.0.113.7:5555"
	return request
}

func TestPublicInteractionsHandlerAcceptsRegisteredEvent(t *testing.T) {
	policies := &fakePublicTelemetryPolicy{policy: enabledPolicy()}
	writer := &fakePublicInteractionWriter{}
	handler := NewPublicInteractionsHandler(policies, writer, wildcardOrigins(t))
	recorder := httptest.NewRecorder()
	handler.HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
	if expected, actual := http.StatusNoContent, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}
	if len(writer.accepted) != 1 || writer.accepted[0].Event != model.PublicInteractionPageView {
		t.Fatalf("accepted = %#v", writer.accepted)
	}
	if len(writer.rejections) != 0 {
		t.Fatalf("rejections = %v, want none", writer.rejections)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}

func TestPublicInteractionsHandlerRejectsDisabledPolicy(t *testing.T) {
	policies := &fakePublicTelemetryPolicy{policy: model.DefaultPublicTelemetryPolicy()}
	writer := &fakePublicInteractionWriter{}
	handler := NewPublicInteractionsHandler(policies, writer, wildcardOrigins(t))
	recorder := httptest.NewRecorder()
	handler.HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
	if expected, actual := http.StatusBadRequest, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}
	if expected, actual := []string{model.PublicRejectionDisabled}, writer.rejections; len(actual) != 1 || actual[0] != expected[0] {
		t.Fatalf("rejections = %v, want %v", actual, expected)
	}
	if len(writer.accepted) != 0 {
		t.Fatalf("accepted = %#v, want none", writer.accepted)
	}
	assertUniformRejection(t, recorder)
}

func TestPublicInteractionsHandlerRejectsInvalidInput(t *testing.T) {
	cases := map[string]struct {
		body, contentType string
		reason            string
	}{
		"unknown event":     {`{"event":"click","surface":"dashboard"}`, "application/json", model.PublicRejectionEvent},
		"unknown surface":   {`{"event":"page_view","surface":"node/1"}`, "application/json", model.PublicRejectionSurface},
		"unknown field":     {`{"event":"page_view","surface":"dashboard","ip":"10.0.0.1"}`, "application/json", model.PublicRejectionBody},
		"malformed json":    {`{"event":`, "application/json", model.PublicRejectionBody},
		"empty body":        {``, "application/json", model.PublicRejectionBody},
		"trailing document": {`{"event":"page_view","surface":"dashboard"}{}`, "application/json", model.PublicRejectionBody},
		"wrong type":        {`{"event":"page_view","surface":"dashboard"}`, "text/plain", model.PublicRejectionContentType},
		"missing type":      {`{"event":"page_view","surface":"dashboard"}`, "", model.PublicRejectionContentType},
		"oversized body":    {`{"event":"page_view","surface":"` + strings.Repeat("a", model.MaximumPublicInteractionBytes) + `"}`, "application/json", model.PublicRejectionBody},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			policies := &fakePublicTelemetryPolicy{policy: enabledPolicy()}
			writer := &fakePublicInteractionWriter{}
			handler := NewPublicInteractionsHandler(policies, writer, wildcardOrigins(t))
			recorder := httptest.NewRecorder()
			handler.HandlePost(recorder, interactionRequest(t, testCase.body, testCase.contentType))
			if expected, actual := http.StatusBadRequest, recorder.Code; expected != actual {
				t.Fatalf("status = %d, want %d", actual, expected)
			}
			if len(writer.rejections) != 1 || writer.rejections[0] != testCase.reason {
				t.Fatalf("rejections = %v, want [%s]", writer.rejections, testCase.reason)
			}
			if len(writer.accepted) != 0 {
				t.Fatalf("accepted = %#v, want none", writer.accepted)
			}
			assertUniformRejection(t, recorder)
		})
	}
}

func TestPublicInteractionsHandlerRejectsUnlistedPolicyEvent(t *testing.T) {
	policy := enabledPolicy()
	policy.AllowedEvents = []string{model.PublicInteractionNodeOpen}
	policies := &fakePublicTelemetryPolicy{policy: policy}
	writer := &fakePublicInteractionWriter{}
	handler := NewPublicInteractionsHandler(policies, writer, wildcardOrigins(t))
	recorder := httptest.NewRecorder()
	handler.HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
	if expected, actual := http.StatusBadRequest, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}
	if len(writer.rejections) != 1 || writer.rejections[0] != model.PublicRejectionEvent {
		t.Fatalf("rejections = %v", writer.rejections)
	}
}

func TestPublicInteractionsHandlerRejectsForeignOrigin(t *testing.T) {
	origins, err := deploymentaccess.ParseOrigins("https://beat.example.com")
	if err != nil {
		t.Fatalf("parse origins: %v", err)
	}
	policies := &fakePublicTelemetryPolicy{policy: enabledPolicy()}
	writer := &fakePublicInteractionWriter{}
	handler := NewPublicInteractionsHandler(policies, writer, origins)
	request := interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json")
	request.Header.Set("Origin", "https://evil.example.com")
	recorder := httptest.NewRecorder()
	handler.HandlePost(recorder, request)
	if expected, actual := http.StatusBadRequest, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}
	if len(writer.rejections) != 1 || writer.rejections[0] != model.PublicRejectionUnauthorized {
		t.Fatalf("rejections = %v", writer.rejections)
	}

	allowed := interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json")
	allowed.Header.Set("Origin", "https://beat.example.com")
	recorder = httptest.NewRecorder()
	handler.HandlePost(recorder, allowed)
	if expected, actual := http.StatusNoContent, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}
}

func TestPublicInteractionsHandlerRateLimitsEachClient(t *testing.T) {
	policies := &fakePublicTelemetryPolicy{policy: enabledPolicy()}
	writer := &fakePublicInteractionWriter{}
	handler := NewPublicInteractionsHandler(policies, writer, wildcardOrigins(t))
	for index := 0; index < model.MaximumPublicInteractionRequestsPerMinute; index++ {
		recorder := httptest.NewRecorder()
		handler.HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
		if expected, actual := http.StatusNoContent, recorder.Code; expected != actual {
			t.Fatalf("request %d status = %d, want %d", index, actual, expected)
		}
	}
	recorder := httptest.NewRecorder()
	handler.HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
	if expected, actual := http.StatusBadRequest, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}
	if len(writer.rejections) != 1 || writer.rejections[0] != model.PublicRejectionRateLimited {
		t.Fatalf("rejections = %v", writer.rejections)
	}
}

func TestPublicInteractionsHandlerReportsFailuresWithoutDetail(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewPublicInteractionsHandler(nil, nil, wildcardOrigins(t)).
		HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
	if expected, actual := http.StatusServiceUnavailable, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}

	policies := &fakePublicTelemetryPolicy{err: errors.New("policy unavailable")}
	recorder = httptest.NewRecorder()
	NewPublicInteractionsHandler(policies, &fakePublicInteractionWriter{}, wildcardOrigins(t)).
		HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
	if expected, actual := http.StatusServiceUnavailable, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}

	writer := &fakePublicInteractionWriter{err: errors.New("mts unavailable")}
	policies = &fakePublicTelemetryPolicy{policy: enabledPolicy()}
	recorder = httptest.NewRecorder()
	NewPublicInteractionsHandler(policies, writer, wildcardOrigins(t)).
		HandlePost(recorder, interactionRequest(t, `{"event":"page_view","surface":"dashboard"}`, "application/json"))
	if expected, actual := http.StatusServiceUnavailable, recorder.Code; expected != actual {
		t.Fatalf("status = %d, want %d", actual, expected)
	}
}

func assertUniformRejection(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var response Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode rejection body: %v", err)
	}
	if expected, actual := publicInteractionRejectionMessage, response.Message; expected != actual {
		t.Fatalf("message = %q, want %q", actual, expected)
	}
}
