package handler

import (
	"net/http"
	"strings"
)

// HTML 无论嵌入还是直接访问，均使用无同源权限的浏览器沙箱。
const sandboxCSP = "sandbox allow-scripts; default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'none'; frame-src 'none'; " +
	"worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

func (handler *ThemeHandler) HandleAsset(w http.ResponseWriter, request *http.Request) {
	digest, name := request.PathValue("digest"), request.PathValue("asset")
	if !isHexDigest(digest) {
		http.NotFound(w, request)
		return
	}
	asset, err := handler.service.ReadAsset(request.Context(), digest, name)
	if err != nil {
		http.NotFound(w, request)
		return
	}
	w.Header().Set("Content-Type", asset.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if asset.MIME == "text/html" {
		w.Header().Set("Content-Security-Policy", sandboxCSP)
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Cache-Control", "no-cache")
	}
	etag := `"` + asset.SHA256 + `"`
	w.Header().Set("ETag", etag)
	if request.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(asset.Content)
}

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	return strings.IndexFunc(value, func(value rune) bool {
		digit := value >= '0' && value <= '9'
		hexadecimal := value >= 'a' && value <= 'f'
		return !digit && !hexadecimal
	}) < 0
}
