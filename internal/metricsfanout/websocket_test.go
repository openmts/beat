package metricsfanout

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func dialRaw(t *testing.T, address, extra string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(address, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(conn, "GET /metrics HTTP/1.1\r\nHost: example.test\r\nConnection: keep-alive, Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nOrigin: https://example.test\r\n%s\r\n", extra)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	return conn, reader, response
}

func readServerFrame(t *testing.T, reader io.Reader) (byte, []byte) {
	t.Helper()
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		t.Fatal(err)
	}
	if header[1]&128 != 0 {
		t.Fatal("server masked frame")
	}
	length, err := frameLength(reader, header[1])
	if err != nil || length > MaximumSnapshotBytes {
		t.Fatal(length, err)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	return header[0] & 15, payload
}

func TestRFCHandshakePingPongAndExactClose(t *testing.T) {
	s, _ := fixture(t, nil)
	s.timing.write = 50 * time.Millisecond
	publishFixture(t, s)
	server := httptest.NewServer(s)
	defer server.Close()
	conn, reader, response := dialRaw(t, server.URL, "")
	if response.StatusCode != 101 || response.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatal(response.Status, response.Header)
	}
	opcode, payload := readServerFrame(t, reader)
	if opcode != 1 || !json.Valid(payload) {
		t.Fatal(opcode)
	}
	if _, err := conn.Write(maskedFrame(9, true, []byte("hello"))); err != nil {
		t.Fatal(err)
	}
	opcode, payload = readServerFrame(t, reader)
	if opcode != 10 || string(payload) != "hello" {
		t.Fatal(opcode, string(payload))
	}
	closePayload := []byte{3, 232}
	if _, err := conn.Write(maskedFrame(8, true, closePayload)); err != nil {
		t.Fatal(err)
	}
	opcode, payload = readServerFrame(t, reader)
	if opcode != 8 || binary.BigEndian.Uint16(payload) != 1000 {
		t.Fatal(opcode, payload)
	}
	await(t, func() bool { return s.Stats().Reserved == 0 })
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManyRealClientsNeverBuildAndXNetInteroperates(t *testing.T) {
	var calls atomic.Int64
	s, clock := fixture(t, nil)
	s.timing.write = 20 * time.Millisecond
	s.config.Build = func(context.Context) (Snapshot, error) {
		calls.Add(1)
		return Snapshot{Timestamp: clock.now(), Value: map[string]any{"nodes": []any{}}}, nil
	}
	publishFixture(t, s)
	server := httptest.NewServer(s)
	defer server.Close()
	for range 4 {
		conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/metrics", "", "https://example.test")
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		var snapshot map[string]any
		if err := websocket.JSON.Receive(conn, &snapshot); err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("client-proportional builder calls", calls.Load())
	}
	await(t, func() bool { return s.Stats().Reserved == 0 })
}

func TestUpgradeLimitsIgnoreUntrustedForwardingHeaders(t *testing.T) {
	s, _ := fixture(t, nil)
	s.limits.perIP = 1
	s.timing.write = 20 * time.Millisecond
	server := httptest.NewServer(s)
	defer server.Close()
	first, _, response := dialRaw(t, server.URL, "X-Forwarded-For: 192.0.2.1\r\n")
	if response.StatusCode != 101 {
		t.Fatal(response.StatusCode)
	}
	_, _, rejected := dialRaw(t, server.URL, "X-Forwarded-For: 192.0.2.2\r\n")
	if rejected.StatusCode != 429 {
		t.Fatal("spoofed IP bypassed limit", rejected.StatusCode)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return s.Stats().Reserved == 0 })
}

func TestMalformedFrameClosesWithoutLeakingReasonData(t *testing.T) {
	s, _ := fixture(t, nil)
	s.timing.write = 20 * time.Millisecond
	server := httptest.NewServer(s)
	defer server.Close()
	conn, reader, response := dialRaw(t, server.URL, "")
	if response.StatusCode != 101 {
		t.Fatal(response.StatusCode)
	}
	if _, err := conn.Write([]byte{129, 0}); err != nil {
		t.Fatal(err)
	}
	opcode, payload := readServerFrame(t, reader)
	if opcode != 8 || binary.BigEndian.Uint16(payload) != 1002 {
		t.Fatal(opcode, payload)
	}
	await(t, func() bool { return s.Stats().Reserved == 0 })
}

func TestCloseWaitsForManagedReaderWriterAndRejectsNewUpgrades(t *testing.T) {
	s, _ := fixture(t, nil)
	s.timing.write = 20 * time.Millisecond
	var mu sync.Mutex
	events := []Event{}
	s.config.Observer = func(event Event) { mu.Lock(); events = append(events, event); mu.Unlock() }
	server := httptest.NewServer(s)
	defer server.Close()
	_, _, response := dialRaw(t, server.URL, "")
	if response.StatusCode != 101 {
		t.Fatal(response.StatusCode)
	}
	var wait sync.WaitGroup
	for range 3 {
		wait.Go(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wait.Wait()
	if state := s.Stats(); state.Reserved != 0 || state.Connections != 0 {
		t.Fatal(state)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test", nil)
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, request)
	if recorder.Code != 503 {
		t.Fatal(recorder.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	count := int64(0)
	for _, event := range events {
		if event.Kind == EventConnection {
			count += event.Count
		}
	}
	if count != 0 {
		t.Fatal("connection lifecycle counted more than once", events)
	}
}
