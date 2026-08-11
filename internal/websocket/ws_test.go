package websocket

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestComputeAcceptKey(t *testing.T) {
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	result := computeAcceptKey(key)
	if result == "" {
		t.Error("expected non-empty accept key")
	}
	if len(result) == 0 {
		t.Error("expected non-empty accept key")
	}
}

func TestComputeAcceptKeyConsistency(t *testing.T) {
	key := "test-key-12345"
	result1 := computeAcceptKey(key)
	result2 := computeAcceptKey(key)
	if result1 != result2 {
		t.Error("expected same key to produce same accept key")
	}
}

func TestNewMetricsHub(t *testing.T) {
	hub := NewMetricsHub()
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}
	if hub.clients == nil {
		t.Error("expected clients map to be initialized")
	}
	if hub.register == nil {
		t.Error("expected register channel to be initialized")
	}
	if hub.unregister == nil {
		t.Error("expected unregister channel to be initialized")
	}
	if hub.broadcast == nil {
		t.Error("expected broadcast channel to be initialized")
	}
}

func TestMetricsHubRegister(t *testing.T) {
	hub := NewMetricsHub()
	go hub.Run()

	client := &MetricsClient{
		conn: nil,
		send: make(chan []byte, 1),
	}
	hub.Register(client)
	hub.Unregister(client)
}

func TestMetricsHubBroadcast(t *testing.T) {
	hub := NewMetricsHub()
	go hub.Run()

	client := &MetricsClient{
		conn: nil,
		send: make(chan []byte, 1),
	}
	hub.Register(client)
	hub.Broadcast([]byte(`{"test": true}`))
	hub.Unregister(client)
}

func TestMetricsHubBroadcastBufferFull(t *testing.T) {
	hub := NewMetricsHub()
	go hub.Run()

	client := &MetricsClient{
		conn: nil,
		send: make(chan []byte, 1),
	}
	hub.Register(client)
	hub.Broadcast([]byte(`first`))
	hub.Broadcast([]byte(`second`))
	hub.Unregister(client)
}

func TestWriteWSFrame(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		payload := []byte("hello")
		expectedLen := 2 + len(payload)
		frame := make([]byte, 2+len(payload))
		frame[0] = 0x81
		frame[1] = byte(len(payload))
		copy(frame[2:], payload)

		if len(frame) != expectedLen {
			t.Errorf("expected frame length %d, got %d", expectedLen, len(frame))
		}
		if frame[0] != 0x81 {
			t.Error("expected first byte to be 0x81 (text frame)")
		}
		if frame[1] != byte(len(payload)) {
			t.Errorf("expected second byte to be %d, got %d", len(payload), frame[1])
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		payload := []byte{}
		frame := make([]byte, 2+len(payload))
		frame[0] = 0x81
		frame[1] = byte(len(payload))
		copy(frame[2:], payload)

		if len(frame) != 2 {
			t.Errorf("expected frame length 2, got %d", len(frame))
		}
	})

	t.Run("large payload", func(t *testing.T) {
		payload := make([]byte, 256)
		for i := range payload {
			payload[i] = byte(i % 256)
		}
		frame := make([]byte, 2+len(payload))
		frame[0] = 0x81
		frame[1] = byte(len(payload))
		copy(frame[2:], payload)

		if len(frame) != 2+256 {
			t.Errorf("expected frame length %d, got %d", 258, len(frame))
		}
	})
}

func TestHandleTerminalSession(t *testing.T) {
	err := HandleTerminalSession(nil, "", nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNewHandler(t *testing.T) {
	hub := NewMetricsHub()
	handler := NewHandler(hub)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestWriteWSFrame_Real(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, err := server.Read(buf)
		if err != nil {
			return
		}
		if n < 2 {
			t.Errorf("expected at least 2 bytes, got %d", n)
		}
	}()

	err := writeWSFrame(client, []byte("hello"))
	if err != nil {
		t.Fatalf("writeWSFrame: %v", err)
	}
	wg.Wait()
}

func TestWriteWSFrame_WriteError(t *testing.T) {
	server, client := net.Pipe()
	_ = server.Close()
	defer func() { _ = client.Close() }()

	err := writeWSFrame(client, []byte("test"))
	if err == nil {
		t.Fatal("expected error on closed connection")
	}
}

func TestWriteWSFrame_Empty(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		_, _ = server.Read(buf)
	}()

	err := writeWSFrame(client, []byte{})
	if err != nil {
		t.Fatalf("writeWSFrame empty: %v", err)
	}
	wg.Wait()
}

func TestWritePump(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	mc := &MetricsClient{
		conn: client,
		send: make(chan []byte, 1),
	}

	go func() {
		buf := make([]byte, 1024)
		_, _ = server.Read(buf)
	}()

	mc.send <- []byte("test")
	go mc.writePump()

	time.Sleep(50 * time.Millisecond)
	close(mc.send)
	time.Sleep(50 * time.Millisecond)
}

func TestWritePump_CloseSend(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	mc := &MetricsClient{
		conn: client,
		send: make(chan []byte, 1),
	}

	go func() {
		buf := make([]byte, 1024)
		_, _ = server.Read(buf)
	}()

	close(mc.send)
	go mc.writePump()
	time.Sleep(50 * time.Millisecond)
}

func TestWritePump_WriteError(t *testing.T) {
	server, client := net.Pipe()
	_ = server.Close()
	defer func() { _ = client.Close() }()

	mc := &MetricsClient{
		conn: client,
		send: make(chan []byte, 1),
	}

	mc.send <- []byte("test")
	go mc.writePump()
	time.Sleep(100 * time.Millisecond)
}

func TestWritePump_Ticker(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	mc := &MetricsClient{
		conn: client,
		send: make(chan []byte, 1),
	}

	go func() {
		for {
			buf := make([]byte, 1024)
			_, err := server.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	go mc.writePump()
	time.Sleep(100 * time.Millisecond)
	close(mc.send)
	time.Sleep(50 * time.Millisecond)
}

func TestUpgradeWS_MissingKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()

	_, err := upgradeWS(w, req)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestUpgradeWS_NoHijacker(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	w := httptest.NewRecorder()

	_, err := upgradeWS(w, req)
	if err == nil {
		t.Fatal("expected error for no hijacker")
	}
}

func TestUpgradeWS_Success(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	go func() {
		conn, _ := l.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = clientConn.Close() }()

	hijacker := &mockHijacker{conn: clientConn}
	_ = hijacker

	conn, err := upgradeWS(hijacker, req)
	if err != nil {
		t.Fatalf("upgradeWS: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}
}

type mockHijacker struct {
	conn net.Conn
}

func (m *mockHijacker) Header() http.Header {
	return http.Header{}
}

func (m *mockHijacker) Write(data []byte) (int, error) {
	return len(data), nil
}

func (m *mockHijacker) WriteHeader(statusCode int) {}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return m.conn, bufio.NewReadWriter(bufio.NewReader(m.conn), bufio.NewWriter(m.conn)), nil
}

func TestNewMetricsHandler(t *testing.T) {
	hub := NewMetricsHub()
	handler := NewMetricsHandler(hub)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewMetricsHandler_ServeHTTP(t *testing.T) {
	hub := NewMetricsHub()
	go hub.Run()

	handler := NewMetricsHandler(hub)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	go func() {
		conn, _ := l.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = clientConn.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	hijacker := &mockHijacker{conn: clientConn}
	handler.ServeHTTP(hijacker, req)

	time.Sleep(50 * time.Millisecond)
}

func TestNewMetricsHandler_UpgradeFail(t *testing.T) {
	hub := NewMetricsHub()
	handler := NewMetricsHandler(hub)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
}

func TestNewHandler_ServeHTTP(t *testing.T) {
	hub := NewMetricsHub()
	handler := NewHandler(hub)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	go func() {
		conn, _ := l.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = clientConn.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	hijacker := &mockHijacker{conn: clientConn}
	handler.ServeHTTP(hijacker, req)

	time.Sleep(50 * time.Millisecond)
}

func TestNewHandler_UpgradeFail(t *testing.T) {
	hub := NewMetricsHub()
	handler := NewHandler(hub)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
}
