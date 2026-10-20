package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/ingress"
)

// IngressLimiter 是准入控制器的最小接口；nil 表示不启用入口准入。
type IngressLimiter interface {
	Admit(class ingress.Class, identity string) (ingress.Lease, *ingress.Rejection)
}

// Admission 在进入处理器之前按固定类别与规范化身份取号。
// 拒绝发生在口令散列、SQLite、MTS、通知与 SSH 之前，不产生排队工作。
func Admission(limiter IngressLimiter, identity func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limiter == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			class, declared := ingress.Classify(r.Method, r.URL.Path)
			if !declared {
				next.ServeHTTP(w, r)
				return
			}
			lease, rejection := limiter.Admit(class, AdmissionIdentity(identity, r))
			if rejection != nil {
				WriteAdmissionRejection(w, rejection)
				return
			}
			defer lease.Release()
			next.ServeHTTP(w, r)
		})
	}
}

// AdmissionIdentity 返回准入键：调用方提供的解析结果优先，否则退化为客户端 IP。
func AdmissionIdentity(identity func(*http.Request) string, r *http.Request) string {
	if identity != nil {
		if value := identity(r); value != "" {
			return value
		}
	}
	return "ip:" + ClientIP(r)
}

// Fingerprint 把会话令牌等敏感值折叠成不可逆的短标识，避免原文进入内存键。
func Fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// WriteAdmissionRejection 输出统一拒绝消息，不泄露桶容量、账号或节点存在性。
func WriteAdmissionRejection(w http.ResponseWriter, rejection *ingress.Rejection) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Retry-After", strconv.Itoa(rejection.RetryAfter))
	w.WriteHeader(rejection.Status)
	_, _ = w.Write([]byte(`{"code":` + strconv.Itoa(rejection.Status) + `,"message":"request rejected"}`))
}
