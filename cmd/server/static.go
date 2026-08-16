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
		fullPath := filepath.Join(staticDir, filepath.FromSlash(requestedPath))
		if _, err := os.Stat(fullPath); err == nil {
			// Hashed build assets are immutable and can be cached long-term;
			// non-hashed files (like index.html) must revalidate so updates
			// reach browsers promptly.
			if strings.HasPrefix(requestedPath, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, request)
			return
		}
		if filepath.Ext(requestedPath) != "" && !strings.Contains(request.Header.Get("Accept"), "text/html") {
			fileServer.ServeHTTP(w, request)
			return
		}

		indexRequest := request.Clone(request.Context())
		indexRequest.URL.Path = "/"
		indexRequest.URL.RawPath = ""
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, indexRequest)
	})
}
