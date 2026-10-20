package notification

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type deliveryRoundTrip func(*http.Request) (*http.Response, error)

func (transport deliveryRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type failedDeliveryBody struct{}

func (failedDeliveryBody) Read([]byte) (int, error) {
	return 0, errors.New("sensitive upstream detail")
}

func (failedDeliveryBody) Close() error { return nil }

func TestDeliveryResponseLimitsAndFailures(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		body     io.ReadCloser
		expected string
	}{
		{"oversized", io.NopCloser(strings.NewReader(strings.Repeat("x", maximumDeliveryResponseBytes+1))), "size limit"},
		{"unreadable", failedDeliveryBody{}, "could not be read"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service := NewService()
			service.httpClient.Transport = deliveryRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: scenario.body}, nil
			})
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://notification.example", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = service.doHTTP(request, "webhook")
			if err == nil || !strings.Contains(err.Error(), scenario.expected) || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("response error: %v", err)
			}
		})
	}
}

func TestTransportFailurePreservesSafeCancellationClassification(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, &net.DNSError{Err: "private endpoint", Name: "private.example", IsTimeout: true}} {
		safe := safeTransportFailure("webhook", failure)
		expected := context.DeadlineExceeded
		if errors.Is(failure, context.Canceled) {
			expected = context.Canceled
		}
		if !errors.Is(safe, expected) || strings.Contains(safe.Error(), "private") {
			t.Fatalf("unsafe error classification: %v", safe)
		}
	}
}
