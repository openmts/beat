package metricsfanout

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ticket, reason := s.reserve(s.config.ClientIP(r))
	if reason != ReasonNone {
		s.reject(w, reason, admissionStatus(reason))
		return
	}
	defer ticket.release()
	if status := validHandshake(r); status != 0 {
		if status == http.StatusUpgradeRequired {
			w.Header().Set("Sec-WebSocket-Version", "13")
		}
		s.reject(w, ReasonHandshake, status)
		return
	}
	if !s.config.AllowOrigin(r) {
		s.reject(w, ReasonOrigin, http.StatusForbidden)
		return
	}
	conn, buffer, err := http.NewResponseController(w).Hijack()
	if err != nil {
		s.reject(w, ReasonHandshake, http.StatusInternalServerError)
		return
	}
	c := newClient(s, conn, buffer.Reader)
	if !s.attach(c) {
		c.terminate(ReasonClosed)
		return
	}
	defer s.detach(c)
	if err := conn.SetWriteDeadline(time.Now().Add(s.timing.write)); err != nil {
		c.terminate(ReasonWrite)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	_, writeErr := fmt.Fprintf(buffer.Writer, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nCache-Control: no-store\r\n\r\n", acceptKey(key))
	flushErr := buffer.Flush()
	if writeErr != nil || flushErr != nil {
		c.terminate(ReasonWrite)
		return
	}
	c.admitted.Store(true)
	s.emit(Event{Kind: EventConnection, Count: 1})
	s.emit(Event{Kind: EventUpgrade, Count: 1})
	c.run()
}

func validHandshake(r *http.Request) int {
	if headerBytes(r) > MaximumHeaderBytes {
		return http.StatusRequestHeaderFieldsTooLarge
	}
	if !validUpgradeRequest(r) {
		return http.StatusBadRequest
	}
	if !headerToken(r.Header, "Connection", "upgrade") || !headerToken(r.Header, "Upgrade", "websocket") {
		return http.StatusBadRequest
	}
	if len(r.Header.Values("Sec-WebSocket-Version")) != 1 || r.Header.Get("Sec-WebSocket-Version") != "13" {
		return http.StatusUpgradeRequired
	}
	if !validHandshakeKey(r.Header) {
		return http.StatusBadRequest
	}
	return 0
}

func headerToken(header http.Header, name, expected string) bool {
	for _, value := range header.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), expected) {
				return true
			}
		}
	}
	return false
}

func acceptKey(key string) string {
	digest := sha1.Sum([]byte(key + acceptGUID))
	return base64.StdEncoding.EncodeToString(digest[:])
}

func admissionStatus(reason Reason) int {
	if reason == ReasonClosed {
		return http.StatusServiceUnavailable
	}
	if reason == ReasonInvalidIP {
		return http.StatusBadRequest
	}
	return http.StatusTooManyRequests
}

func (s *Service) reject(w http.ResponseWriter, reason Reason, status int) {
	s.emit(Event{Kind: EventUpgrade, Reason: reason, Count: 1})
	http.Error(w, rejectionMessage(reason), status)
}

// rejectionMessage 让非浏览器客户端与运维能从响应体区分失败原因；
// 浏览器不会把响应体暴露给页面脚本，因此这里只补充稳定的原因标识。
func rejectionMessage(reason Reason) string {
	if strings.TrimSpace(string(reason)) == "" {
		return "metrics stream unavailable"
	}
	return "metrics stream unavailable: " + string(reason)
}

func headerBytes(r *http.Request) int {
	size := len(r.Host) + len(r.RequestURI)
	for name, values := range r.Header {
		for _, value := range values {
			size += len(name) + len(value) + 4
		}
	}
	return size
}

func validUpgradeRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && r.ProtoMajor == 1 && r.ProtoMinor >= 1 && r.ContentLength <= 0 && len(r.TransferEncoding) == 0
}

func validHandshakeKey(header http.Header) bool {
	if len(header.Values("Sec-WebSocket-Key")) != 1 {
		return false
	}
	key := strings.TrimSpace(header.Get("Sec-WebSocket-Key"))
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 16 || base64.StdEncoding.EncodeToString(decoded) != key {
		return false
	}
	return true
}
