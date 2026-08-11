package websocket

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type MetricsClient struct {
	conn net.Conn
	send chan []byte
}

type MetricsHub struct {
	mu      sync.RWMutex
	clients map[*MetricsClient]struct{}

	register   chan *MetricsClient
	unregister chan *MetricsClient
	broadcast  chan []byte
}

func NewMetricsHub() *MetricsHub {
	return &MetricsHub{
		clients:    make(map[*MetricsClient]struct{}),
		register:   make(chan *MetricsClient),
		unregister: make(chan *MetricsClient),
		broadcast:  make(chan []byte, 256),
	}
}

func (h *MetricsHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case data := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *MetricsHub) Register(client *MetricsClient) {
	h.register <- client
}

func (h *MetricsHub) Unregister(client *MetricsClient) {
	h.unregister <- client
}

func (h *MetricsHub) Broadcast(metricData []byte) {
	h.broadcast <- metricData
}

func (c *MetricsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
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

		client := &MetricsClient{
			conn: conn,
			send: make(chan []byte, 256),
		}
		hub.Register(client)
		go client.writePump()
	})
}

type TerminalWebSocket struct{}

func NewHandler(hub *MetricsHub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeWS(w, r)
		if err != nil {
			slog.ErrorContext(r.Context(), "metrics websocket upgrade failed", "path", r.URL.Path, "error", err)
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

	acceptKey := computeAcceptKey(key)

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack: %w", err)
	}

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n" +
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
	hasher := sha256.New()
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
