package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func newSPAHandler(staticDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			fileServer.ServeHTTP(w, request)
			return
		}
		requestedPath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if requestedPath != "" && requestedPath != "index.html" {
			fullPath := filepath.Join(staticDir, filepath.FromSlash(requestedPath))
			if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
				if requestedPath == "sw.js" {
					serveUncachedFile(w, request, fileServer, "sw.js")
					return
				}
				setStaticCacheControl(w, requestedPath)
				fileServer.ServeHTTP(w, request)
				return
			}
			if filepath.Ext(requestedPath) != "" && !strings.Contains(request.Header.Get("Accept"), "text/html") {
				fileServer.ServeHTTP(w, request)
				return
			}
		}
		serveShell(w, request, fileServer)
	})
}

// serveShell 始终把 SPA 外壳当作最新内容下发。HTML 一旦允许条件缓存，
// 旧发布即可在后续版本中继续命中 304，长期驻留页会一直运行旧分片。
func serveShell(w http.ResponseWriter, request *http.Request, fileServer http.Handler) {
	shell := request.Clone(request.Context())
	shell.URL.Path = "/"
	shell.URL.RawPath = ""
	stripConditionalHeaders(shell)
	w.Header().Set("Cache-Control", "no-store")
	fileServer.ServeHTTP(w, shell)
}

func serveUncachedFile(w http.ResponseWriter, request *http.Request, fileServer http.Handler, name string) {
	next := request.Clone(request.Context())
	next.URL.Path = "/" + name
	next.URL.RawPath = ""
	stripConditionalHeaders(next)
	w.Header().Set("Cache-Control", "no-store")
	fileServer.ServeHTTP(w, next)
}

func stripConditionalHeaders(request *http.Request) {
	for _, header := range []string{"If-Match", "If-Modified-Since", "If-None-Match", "If-Unmodified-Since"} {
		request.Header.Del(header)
	}
}

func setStaticCacheControl(w http.ResponseWriter, requestedPath string) {
	if strings.HasPrefix(requestedPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}
