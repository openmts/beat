package middleware

import (
	"mime"
	"net/http"
	"strings"
)

// JSONBodyLimit 在进入认证与处理器之前，把声明为 JSON 的请求体装进有界读取器。
//
// 只有 Content-Type 为 application/json 的请求会被限制；上传、归档、主题包等
// 非 JSON 载荷继续使用各自路由声明的预算，不会被这里误伤。缺失或非 JSON 的
// Content-Type 由解码助手统一按不支持的媒体类型拒绝。
func JSONBodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxBytes > 0 && isJSONRequest(r.Header.Get("Content-Type")) {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isJSONRequest(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	return err == nil && mediaType == "application/json"
}
