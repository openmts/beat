package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiagnosticAgentHandlerRejections(t *testing.T) {
	handler := NewDiagnosticHandler(&fakeDiagnostics{})
	cases := []struct {
		name   string
		handle http.HandlerFunc
	}{
		{"advertise", handler.HandleDiagnosticAdvertise},
		{"lease", handler.HandleDiagnosticLease},
		{"start", handler.HandleDiagnosticStart},
		{"heartbeat", handler.HandleDiagnosticHeartbeat},
		{"progress", handler.HandleDiagnosticProgress},
		{"complete", handler.HandleDiagnosticComplete},
	}
	for _, testCase := range cases {
		t.Run(testCase.name+"/malformed body", func(t *testing.T) {
			w := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad"))
			testCase.handle(w, request)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("malformed body status = %d", w.Code)
			}
		})
	}
	for index := 0; index < cap(handler.agentSlots); index++ {
		handler.agentSlots <- struct{}{}
	}
	for _, testCase := range cases {
		t.Run(testCase.name+"/saturated", func(t *testing.T) {
			w := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
			testCase.handle(w, request)
			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("saturated slot status = %d", w.Code)
			}
		})
	}
	for index := 0; index < cap(handler.agentSlots); index++ {
		handler.releaseAgent()
	}
	w := httptest.NewRecorder()
	handler.HandleDiagnosticLease(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"capability_digest":"digest"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("released slot status = %d", w.Code)
	}
}
