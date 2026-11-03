package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

type slowRemoteGeoIP struct {
	geoIPStub
	delay time.Duration
}

func (service slowRemoteGeoIP) InstallRemoteMMDB(ctx context.Context, _ geoip.Actor, _ geoip.RemoteMMDBInstall) (model.GeoIPSettings, error) {
	timer := time.NewTimer(service.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return model.GeoIPSettings{}, ctx.Err()
	case <-timer.C:
		return model.GeoIPSettings{}, nil
	}
}

func TestRemoteMMDBInstallOverridesShortServerWriteTimeout(t *testing.T) {
	handler := NewGeoIPHandler(slowRemoteGeoIP{delay: 50 * time.Millisecond})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := model.AdminPrincipal{User: model.AdminUser{ID: "owner"}, Session: model.AdminSession{ID: "session"}}
		handler.HandleRemoteMMDBInstall(w, r.WithContext(middleware.WithAdminPrincipal(r.Context(), principal)))
	}))
	server.Config.WriteTimeout = 10 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)

	body := `{"version":"2026.10.22","sha256":"` + strings.Repeat("a", 64) + `","expected_revision":1}`
	response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", response.StatusCode, payload, err)
	}
}

func TestRemoteMMDBInstallDeadlineIsBoundedAndFailsClosed(t *testing.T) {
	handler := NewGeoIPHandler(geoIPStub{})
	body := `{"version":"2026.10.22","sha256":"` + strings.Repeat("a", 64) + `","expected_revision":1}`
	writer := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	handler.HandleRemoteMMDBInstall(writer, geoIPRequest(body))
	if writer.Code != http.StatusOK || time.Until(writer.deadline) < 119*time.Second || time.Until(writer.deadline) > 121*time.Second {
		t.Fatalf("status=%d deadline=%s", writer.Code, writer.deadline)
	}
	writer = &deadlineWriter{ResponseRecorder: httptest.NewRecorder(), err: errors.New("fixture deadline failure")}
	handler.HandleRemoteMMDBInstall(writer, geoIPRequest(body))
	if writer.Code != http.StatusServiceUnavailable {
		t.Fatalf("deadline failure status=%d", writer.Code)
	}
}
