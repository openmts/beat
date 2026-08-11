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
		fileServer.ServeHTTP(w, indexRequest)
	})
}
