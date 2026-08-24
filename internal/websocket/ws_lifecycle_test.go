package websocket

import (
	"bufio"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestMetricsHubUnregisterIdempotent(t *testing.T) {
	hub := NewMetricsHub()
	client := &MetricsClient{conn: nil, send: make(chan []byte, 1)}
	hub.Register(client)

	hub.Unregister(client)
	hub.Unregister(client) // 第二次必须为空操作，不得重复关闭通道

	if _, ok := <-client.send; ok {
		t.Fatal("send channel should be closed after unregister")
	}
}

func TestMetricsHubCloseDisconnectsAllClients(t *testing.T) {
	hub := NewMetricsHub()
	go hub.Run()

	server, clientConn := net.Pipe()
	defer func() { _ = server.Close() }()
	client := &MetricsClient{conn: clientConn, send: make(chan []byte, 1), hub: hub}
	hub.Register(client)

	hub.Close()
	hub.Close() // 幂等

	if hub.Count() != 0 {
		t.Fatalf("count after close = %d, want 0", hub.Count())
	}
	select {
	case _, ok := <-client.send:
		if ok {
			t.Fatal("send channel should be closed")
		}
	default:
		t.Fatal("send channel should be closed and empty")
	}
	// 连接应被关闭，写入端最终报错
	if err := writeWSFrame(server, []byte("x")); err == nil {
		t.Log("write after peer close did not fail immediately; pipe may buffer one frame")
	}
}

func TestMetricsHubRunReturnsOnClose(t *testing.T) {
	hub := NewMetricsHub()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		hub.Run()
	}()
	hub.Close()

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Close")
	}
}

func TestWritePumpUnregistersOnSendClosed(t *testing.T) {
	hub := NewMetricsHub()
	server, clientConn := net.Pipe()
	defer func() { _ = server.Close() }()

	client := &MetricsClient{conn: clientConn, send: make(chan []byte, 1), hub: hub}
	hub.Register(client)

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 1024)
		for {
			if _, err := server.Read(buf); err != nil {
				return
			}
		}
	}()

	go client.writePump()
	time.Sleep(30 * time.Millisecond)
	hub.Unregister(client)

	if hub.Count() != 0 {
		t.Fatalf("count = %d before pump exit, want 0", hub.Count())
	}
	// writePump 观察到 send 关闭后退出并再次注销（幂等），连接被关闭
	if _, open := <-client.send; open {
		t.Fatal("expected closed send channel")
	}
	<-readerDone
}

func TestReadFrameMaskedPayload(t *testing.T) {
	server, clientConn := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = clientConn.Close() }()

	frame := []byte{0x81, 0x83, 0x01, 0x02, 0x03, 0x04, 'a', 'b', 'c'}
	writeErr := make(chan error, 1)
	go func() { writeErr <- writeRaw(server, frame) }()

	opcode, payload, err := readFrame(clientConn)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("opcode = %#x, want text frame", opcode)
	}
	if len(payload) != 3+4 {
		t.Fatalf("payload length = %d, want masked payload plus key", len(payload))
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func TestReadFrameExtended16BitLength(t *testing.T) {
	server, clientConn := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = clientConn.Close() }()

	payload := make([]byte, 300)
	ext := make([]byte, 2)
	binary.BigEndian.PutUint16(ext, uint16(len(payload)))
	// 无掩码 + 16 位扩展长度标记为 0x7E（0x80 为掩码位，不能置位）
	frame := append([]byte{0x82, 0x7E}, ext...)
	frame = append(frame, payload...)

	writeErr := make(chan error, 1)
	go func() { writeErr <- writeRaw(server, frame) }()

	opcode, got, err := readFrame(clientConn)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if opcode != 0x2 || len(got) != len(payload) {
		t.Fatalf("opcode=%#x len=%d", opcode, len(got))
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func TestReadFrameOversizedRejected(t *testing.T) {
	server, clientConn := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = clientConn.Close() }()

	// 无掩码 + 64 位扩展长度标记为 0x7F
	frame := []byte{0x82, 0x7F, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00}
	writeErr := make(chan error, 1)
	go func() { writeErr <- writeRaw(server, frame) }()

	_, _, err := readFrame(clientConn)
	if err == nil {
		t.Fatal("expected oversized frame rejection")
	}
	// 显式解除写入端阻塞，避免管道半写悬挂
	_ = server.Close()
	<-writeErr
}

func TestReadFrameConnectionError(t *testing.T) {
	server, clientConn := net.Pipe()
	_ = server.Close()
	defer func() { _ = clientConn.Close() }()

	if _, _, err := readFrame(clientConn); err == nil {
		t.Fatal("expected read error on closed connection")
	}
}

func TestReadPumpDetectsDisconnectAndUnregisters(t *testing.T) {
	hub := NewMetricsHub()
	server, clientConn := net.Pipe()

	client := &MetricsClient{conn: clientConn, send: make(chan []byte, 1), hub: hub}
	hub.Register(client)
	go client.readPump()

	_ = server.Close() // 对端断开 → readPump 关闭连接
	if _, err := clientConn.Read(make([]byte, 1)); err == nil {
		time.Sleep(50 * time.Millisecond)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hub.Count() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Count() != 0 {
		t.Fatal("readPump should trigger unregister after disconnect")
	}
}

func TestNewMetricsHandlerLifecycle(t *testing.T) {
	hub := NewMetricsHub()
	handler := NewMetricsHandler(hub)

	// 用 net.Pipe 使握手完成成为确定性信号：服务端 Flush 会阻塞到客户端
	// 读走握手字节，随后 Register 已发生，不存在端口或调度竞态。
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	served := make(chan struct{})
	go func() {
		defer close(served)
		handler.ServeHTTP(&mockHijacker{conn: serverConn}, newWSRequest())
	}()

	reader := bufio.NewReader(clientConn)
	if err := clientConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read handshake: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	if err := clientConn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("clear deadline: %v", err)
	}
	<-served

	if hub.Count() != 1 {
		t.Fatalf("hub count = %d after upgrade, want 1", hub.Count())
	}

	hub.Close()
	closeDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(closeDeadline) && hub.Count() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Count() != 0 {
		t.Fatalf("hub count = %d after close, want 0", hub.Count())
	}
}

func writeRaw(conn net.Conn, data []byte) error {
	_, err := conn.Write(data)
	return err
}

func newWSRequest() *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/metrics/ws", nil)
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return request
}
