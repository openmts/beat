package agentrollouthttp

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

type deadlineFaultWriter struct {
	*httptest.ResponseRecorder
	readErr, writeErr error
}

func (w deadlineFaultWriter) SetReadDeadline(time.Time) error { return w.readErr }

func (w deadlineFaultWriter) SetWriteDeadline(time.Time) error { return w.writeErr }

func TestTransferDeadlineErrorsFailClosedAndUnsupportedWritersRemainUsable(t *testing.T) {
	failure := errors.New("closed connection")
	for _, entry := range []struct{ readErr, writeErr, errorExpected error }{{nil, nil, nil}, {http.ErrNotSupported, http.ErrNotSupported, nil}, {failure, nil, agentrollout.ErrUnavailable}, {nil, failure, agentrollout.ErrUnavailable}} {
		writer := deadlineFaultWriter{ResponseRecorder: httptest.NewRecorder(), readErr: entry.readErr, writeErr: entry.writeErr}
		if err := transferDeadline(writer, time.Now().Add(time.Second), true); err != entry.errorExpected {
			t.Fatal(err, entry.errorExpected)
		}
	}
}

func TestUploadDeadlineFailureDoesNotPublishArtifact(t *testing.T) {
	f := newFixture(t)
	release, err := f.s.Import(t.Context(), f.actor, f.signed)
	if err != nil {
		t.Fatal(err)
	}
	request := f.deadlineRequest("PUT", "/api/v1/admin/agent-releases/"+release.ID+"/artifacts/linux-amd64", f.artifact)
	request.Header.Set("Content-Type", "application/octet-stream")
	mux := http.NewServeMux()
	New(f.s).Register(mux)
	response := deadlineFaultWriter{ResponseRecorder: httptest.NewRecorder(), writeErr: errors.New("write deadline failed")}
	mux.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatal(response.Code, response.Body.String())
	}
	var state string
	if err := f.db.DB.QueryRow("SELECT state FROM agent_release_artifacts WHERE release_id=?", release.ID).Scan(&state); err != nil || state != "missing" {
		t.Fatal(state, err)
	}
}

func TestDownloadDeadlineFailureDoesNotStreamArtifact(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	identity := f.identities[targets[0].NodeID]
	assignment, err := f.s.Lease(t.Context(), identity, capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	request := f.deadlineRequest("GET", "/api/v1/agents/rollout/artifacts/"+assignment.ID+"/linux-amd64", nil)
	request = request.WithContext(middleware.WithAgentIdentity(request.Context(), identity))
	request.Header.Set("X-Beat-Rollout-Nonce", assignment.Nonce)
	mux := http.NewServeMux()
	New(f.s).Register(mux)
	response := deadlineFaultWriter{ResponseRecorder: httptest.NewRecorder(), writeErr: errors.New("write deadline failed")}
	mux.ServeHTTP(response, request)
	if response.Code != 503 || bytes.Equal(response.Body.Bytes(), f.artifact) {
		t.Fatal(response.Code, response.Body.String())
	}
}

func (f *fixture) deadlineRequest(method, path string, body []byte) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{User: model.AdminUser{ID: f.actor.UserID}, Session: model.AdminSession{ID: f.actor.SessionID}}))
}
