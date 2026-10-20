package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/service"
)

type revocationTerminal struct {
	closed  chan struct{}
	release chan struct{}
}

func (terminal *revocationTerminal) OpenTerminal(ctx context.Context, _ string, stream io.ReadWriteCloser) error {
	defer close(terminal.closed)
	if _, err := stream.Write([]byte("ready")); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-terminal.release:
		return nil
	}
}

func (*revocationTerminal) ExecuteBatch(context.Context, []string, string) []service.BatchResult {
	return nil
}

func TestActiveTerminalClosesOnLogout(t *testing.T) {
	terminal := &revocationTerminal{closed: make(chan struct{}), release: make(chan struct{})}
	router := setupSessionRouterWithOptions(t, WithTerminalOperations(terminal))
	cookie := bootstrapTerminalOwner(t, router)
	server := httptest.NewServer(router.ServeHandler())
	t.Cleanup(server.Close)
	connection := connectRevocationTerminal(t, server.URL, cookie)
	t.Cleanup(func() { close(terminal.release); _ = connection.Close() })
	response := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/logout", "", cookie, "http://example.com")
	if response.Code != http.StatusOK {
		t.Fatalf("logout status = %d", response.Code)
	}
	select {
	case <-terminal.closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("authenticated SSH terminal survived logout")
	}
}

func bootstrapTerminalOwner(t *testing.T, router *Router) *http.Cookie {
	t.Helper()
	body := `{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner","password":"correct horse battery staple"}`
	response := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap", body, "", "http://example.com")
	if response.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d", response.Code)
	}
	return response.Result().Cookies()[0]
}

func connectRevocationTerminal(t *testing.T, address string, cookie *http.Cookie) *websocket.Conn {
	t.Helper()
	config, err := websocket.NewConfig("ws"+address[4:]+"/api/v1/ws/terminal?node_id=test", address)
	if err != nil {
		t.Fatal(err)
	}
	config.Header.Set("Cookie", cookie.String())
	connection, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	var ready string
	if err := websocket.Message.Receive(connection, &ready); err != nil || ready != "ready" {
		_ = connection.Close()
		t.Fatalf("terminal readiness = %q, %v", ready, err)
	}
	return connection
}
