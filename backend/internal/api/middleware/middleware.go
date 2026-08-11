package middleware

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicCORSPath(r.URL.Path) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions && publicCORSPath(r.URL.Path) {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func publicCORSPath(path string) bool {
	if path == "/api/v1/settings/site" || path == "/api/v1/groups" || path == "/api/v1/nodes" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/network/quality") {
		return true
	}
	if !strings.HasPrefix(path, "/api/v1/nodes/") {
		return false
	}
	return !strings.Contains(path, "/install") && !strings.Contains(path, "/token/") &&
		path != "/api/v1/nodes/manage" && path != "/api/v1/nodes/report"
}

func ContentTypeJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(content []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(content)
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func Logging(next http.Handler) http.Handler {
	return LoggingWithObserver(next, nil)
}

type HTTPObserver interface {
	ObserveHTTP(method, route string, status int, duration time.Duration)
}

func LoggingWithObserver(next http.Handler, observer HTTPObserver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		route := r.Pattern
		if observer != nil {
			observer.ObserveHTTP(r.Method, route, rw.statusCode, duration)
		}
		slog.InfoContext(r.Context(), "http request",
			"request_id", RequestID(r.Context()), "method", r.Method, "route", route,
			"path", r.URL.Path, "status", rw.statusCode, "duration_ms", duration.Milliseconds(),
			"client_ip", ClientIP(r))
	})
}

func ObserveStatus(next http.Handler, after func(int)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, request)
		after(rw.statusCode)
	})
}

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.ErrorContext(r.Context(), "panic recovered", "request_id", RequestID(r.Context()),
					"error", fmt.Sprint(err), "stack", string(debug.Stack()))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				http.Error(w, `{"code": 500, "message": "internal server error"}`, http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
