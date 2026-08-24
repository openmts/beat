package websocket

import (
	"crypto/sha1" //nolint:gosec // RFC 6455 规定握手摘要必须使用 SHA-1
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxFramePayload 限制单个入站帧的最大长度，防止恶意帧耗尽内存。
const maxFramePayload = 1 << 20

type MetricsClient struct {
	conn net.Conn
	send chan []byte
	hub  *MetricsHub
}

type MetricsHub struct {
	mu       sync.RWMutex
	clients  map[*MetricsClient]struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func NewMetricsHub() *MetricsHub {
	return &MetricsHub{
		clients: make(map[*MetricsClient]struct{}),
		done:    make(chan struct{}),
	}
}

// Run 阻塞至 Hub 关闭；客户端注册与广播由锁直接保护，不再依赖内部通道。
func (h *MetricsHub) Run() {
	<-h.done
}

func (h *MetricsHub) Register(client *MetricsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = struct{}{}
}

// Unregister 幂等：仅在客户端仍注册时移除并关闭其发送通道，避免重复关闭。
func (h *MetricsHub) Unregister(client *MetricsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[client]; !ok {
		return
	}
	delete(h.clients, client)
	close(client.send)
}

func (h *MetricsHub) Broadcast(metricData []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- metricData:
		default:
		}
	}
}

// Count 返回当前注册的客户端数量。
func (h *MetricsHub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close 关闭 Hub 并断开全部客户端连接；幂等且关闭后拒绝新注册。
func (h *MetricsHub) Close() {
	h.stopOnce.Do(func() {
		close(h.done)
		h.mu.RLock()
		clients := make([]*MetricsClient, 0, len(h.clients))
		for client := range h.clients {
			clients = append(clients, client)
		}
		h.mu.RUnlock()
		for _, client := range clients {
			_ = client.conn.Close()
			h.Unregister(client)
		}
	})
}

func (c *MetricsClient) readPump() {
	for {
		if _, _, err := readFrame(c.conn); err != nil {
			_ = c.conn.Close()
			if c.hub != nil {
				c.hub.Unregister(c)
			}
			return
		}
	}
}

func (c *MetricsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
		if c.hub != nil {
			c.hub.Unregister(c)
		}
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				return
			}
			if err := writeWSFrame(c.conn, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := writeWSFrame(c.conn, []byte{}); err != nil {
				return
			}
		}
	}
}

func NewMetricsHandler(hub *MetricsHub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeWS(w, r)
		if err != nil {
			slog.ErrorContext(r.Context(), "websocket upgrade failed", "path", r.URL.Path, "error", err)
			return
		}

		client := &MetricsClient{conn: conn, send: make(chan []byte, 256), hub: hub}
		hub.Register(client)
		go client.readPump()
		go client.writePump()
	})
}

type TerminalWebSocket struct{}

func NewHandler(_ *MetricsHub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeWS(w, r)
		if err != nil {
			slog.ErrorContext(r.Context(), "websocket upgrade failed", "path", r.URL.Path, "error", err)
			return
		}
		_ = conn.Close()
	})
}

func HandleTerminalSession(conn net.Conn, nodeID string, sshConfig *ssh.ClientConfig) error {
	_ = conn
	_ = nodeID
	_ = sshConfig
	return nil
}

func upgradeWS(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("missing Sec-WebSocket-Key header")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("server does not support hijacking")
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack: %w", err)
	}

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + computeAcceptKey(key) + "\r\n" +
		"\r\n"

	if _, err := bufrw.WriteString(response); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("write response: %w", err)
	}
	if err := bufrw.Flush(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("flush response: %w", err)
	}

	return conn, nil
}

func computeAcceptKey(key string) string {
	hasher := sha1.New()
	hasher.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}

func writeWSFrame(conn net.Conn, payload []byte) error {
	frame := make([]byte, 2+len(payload))
	frame[0] = 0x81
	frame[1] = byte(len(payload))
	copy(frame[2:], payload)

	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	return nil
}

// readFrame 读取并跳过一个完整的 WebSocket 帧，仅用于对端活性检测。
// 客户端帧按规范必须带掩码，掩码键包含在负载内一并读取丢弃。
func readFrame(conn net.Conn) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, fmt.Errorf("read frame header: %w", err)
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := int(header[1] & 0x7f)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(conn, ext); err != nil {
			return 0, nil, fmt.Errorf("read extended length: %w", err)
		}
		length = int(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(conn, ext); err != nil {
			return 0, nil, fmt.Errorf("read extended length: %w", err)
		}
		size := binary.BigEndian.Uint64(ext)
		if size > maxFramePayload {
			return 0, nil, fmt.Errorf("frame payload %d exceeds limit", size)
		}
		length = int(size)
	}
	if masked {
		length += 4
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, fmt.Errorf("read frame payload: %w", err)
	}
	return opcode, payload, nil
}
