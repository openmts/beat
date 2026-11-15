package server

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"beat/internal/platform/sqlite"

	"github.com/go-chi/chi/v5/middleware"
)

const csrfHeader = "X-Beat-CSRF"

type adminIdentity struct {
	Username  string
	CSRFToken string
}

type adminIdentityKey struct{}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		if requestIsHTTPS(r) {
			headers.Set("Cross-Origin-Opener-Policy", "same-origin")
			headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rejectCrossOriginMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isUnsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if fetchSite := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); strings.EqualFold(fetchSite, "cross-site") {
			writeError(w, http.StatusForbidden, "cross_origin_rejected", "已拒绝跨站请求")
			return
		}
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
				writeError(w, http.StatusForbidden, "cross_origin_rejected", "已拒绝跨站请求")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request, identity adminIdentity) bool {
	if !isUnsafeMethod(r.Method) {
		return true
	}
	provided := strings.TrimSpace(r.Header.Get(csrfHeader))
	if provided == "" || identity.CSRFToken == "" || !constantTimeEqual(provided, identity.CSRFToken) {
		writeError(w, http.StatusForbidden, "csrf_invalid", "请求校验失败，请刷新页面后重试")
		return false
	}
	return true
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func (s *Server) auditAdminMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isUnsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		writer := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(writer, r)
		identity, _ := r.Context().Value(adminIdentityKey{}).(adminIdentity)
		s.audit(r, "admin_mutation", identity.Username, map[string]any{
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      writer.Status(),
			"duration_ms": time.Since(started).Milliseconds(),
		})
	})
}

func (s *Server) audit(r *http.Request, event, actor string, details map[string]any) {
	if s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entry := sqlite.AuditEntry{
		Event: event, Actor: actor, Method: r.Method, Path: r.URL.Path,
		RemoteAddr: clientAddress(r), UserAgent: r.UserAgent(), Details: details,
	}
	if writer, ok := details["status"].(int); ok {
		entry.StatusCode = writer
	}
	if err := s.store.AppendAudit(ctx, entry); err != nil {
		s.logger.Warn("audit_write_failed", "event", event, "error", err)
	}
}

func (s *Server) auditResponse(r *http.Request, event, actor string, status int, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	details["status"] = status
	s.audit(r, event, actor, details)
}

func (s *Server) runMaintenance(ctx context.Context) {
	s.cleanupExpired(context.Background())
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpired(context.Background())
		}
	}
}

func (s *Server) cleanupExpired(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := s.store.Cleanup(cleanupCtx, time.Now().UTC(), 30*24*time.Hour, 90*24*time.Hour)
	if err != nil {
		s.logger.Warn("storage_cleanup_failed", "error", err)
		return
	}
	if result.AdminSessions+result.EnrollmentTokens+result.TaskRuns+result.AgentTasks+result.AgentTombstones+result.AuditEntries > 0 {
		s.logger.Info("storage_cleanup_completed",
			"admin_sessions", result.AdminSessions,
			"enrollment_tokens", result.EnrollmentTokens,
			"task_runs", result.TaskRuns,
			"agent_tasks", result.AgentTasks,
			"agent_tombstones", result.AgentTombstones,
			"audit_entries", result.AuditEntries,
		)
	}
}

func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

type loginAttempt struct {
	Failures     int
	WindowStart  time.Time
	BlockedUntil time.Time
}

type loginLimiter struct {
	mu          sync.Mutex
	attempts    map[string]loginAttempt
	lastCleanup time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastCleanup) > 10*time.Minute {
		for candidate, attempt := range l.attempts {
			if now.After(attempt.BlockedUntil) && now.Sub(attempt.WindowStart) > 30*time.Minute {
				delete(l.attempts, candidate)
			}
		}
		l.lastCleanup = now
	}
	attempt := l.attempts[key]
	if now.Before(attempt.BlockedUntil) {
		return false, attempt.BlockedUntil.Sub(now)
	}
	return true, 0
}

func (l *loginLimiter) failure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) > 15*time.Minute {
		attempt = loginAttempt{WindowStart: now}
	}
	attempt.Failures++
	if attempt.Failures >= 5 {
		attempt.BlockedUntil = now.Add(15 * time.Minute)
		attempt.Failures = 0
		attempt.WindowStart = now
	}
	l.attempts[key] = attempt
}

func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func formatRetryAfter(duration time.Duration) string {
	seconds := int(duration.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
