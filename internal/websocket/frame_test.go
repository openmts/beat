package websocket

import (
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	xws "golang.org/x/net/websocket"
)

func TestExposedMetricsSocketEncodesExtendedPayloadLengths(t *testing.T) {
	hub := NewMetricsHub()
	t.Cleanup(hub.Close)
	server := httptest.NewServer(NewMetricsHandler(hub))
	t.Cleanup(server.Close)
	connection, err := xws.Dial("ws"+server.URL[4:], "", "http://localhost/")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for hub.Count() == 0 {
		time.Sleep(time.Millisecond)
	}
	for _, length := range []int{0, 125, 126, 65535, 65536} {
		payload := strings.Repeat("x", length)
		hub.Broadcast([]byte(payload))
		var received string
		if err := xws.Message.Receive(connection, &received); err != nil {
			t.Fatal("invalid public metrics WS frame", length, err)
		}
		if payload != received {
			t.Fatal("truncated metrics snapshot", length)
		}
	}
}

type shortFrameConnection struct {
	net.Conn
	zero bool
}

func (connection shortFrameConnection) Write(data []byte) (int, error) {
	if connection.zero {
		return 0, nil
	}
	return connection.Conn.Write(data[:min(len(data), 3)])
}

func TestFrameWriterCompletesPartialWritesAndRejectsOversize(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	done := make(chan error, 1)
	go func() { done <- writeWSFrame(shortFrameConnection{Conn: server}, []byte("payload")) }()
	_, payload, err := readFrame(client)
	if err != nil || string(payload) != "payload" {
		t.Fatal("short write corrupted frame", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := writeWSFrame(shortFrameConnection{zero: true}, nil); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("zero progress looped", err)
	}
	if _, err := encodeTextFrame(make([]byte, maxFramePayload+1)); err == nil {
		t.Fatal("unbounded outbound frame")
	}
}
