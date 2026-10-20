package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

const publicManifestLimit = 32 * 1024

const publicManifestNameLimit = 80

const publicManifestShortNameLimit = 24

type publicManifestSettings interface {
	Get(context.Context) (model.SiteSettings, error)
}

type publicManifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

type publicManifest struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	ShortName  string               `json:"short_name"`
	StartURL   string               `json:"start_url"`
	Scope      string               `json:"scope"`
	Display    string               `json:"display"`
	Direction  string               `json:"dir"`
	Language   string               `json:"lang"`
	Background string               `json:"background_color"`
	Theme      string               `json:"theme_color"`
	Icons      []publicManifestIcon `json:"icons"`
}

func registerPublicManifest(mux *http.ServeMux, staticDir string, settings publicManifestSettings) {
	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		manifest, err := readPublicManifest(filepath.Join(staticDir, "manifest.webmanifest"))
		if err != nil {
			http.Error(w, "public manifest unavailable", http.StatusServiceUnavailable)
			return
		}
		site, err := settings.Get(r.Context())
		if err != nil {
			http.Error(w, "public settings unavailable", http.StatusServiceUnavailable)
			return
		}
		title := cleanManifestTitle(site.SiteTitle)
		if title == "" {
			http.Error(w, "public title unavailable", http.StatusServiceUnavailable)
			return
		}
		manifest.Name = truncateManifestTitle(title, publicManifestNameLimit)
		manifest.ShortName = truncateManifestTitle(title, publicManifestShortNameLimit)
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(manifest)
	})
}

func readPublicManifest(path string) (publicManifest, error) {
	var result publicManifest
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, publicManifestLimit+1))
	if err != nil {
		return result, err
	}
	if len(data) > publicManifestLimit || !utf8.Valid(data) {
		return result, errors.New("invalid manifest bytes")
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if !validPublicManifest(result) {
		return result, errors.New("invalid public manifest")
	}
	return result, nil
}

var publicIconPath = regexp.MustCompile(`^/assets/beat-(192|512)-[a-f0-9]{12}\.png$`)

func validPublicManifest(value publicManifest) bool {
	if value.ID != "/" || value.StartURL != "/" || value.Scope != "/" || value.Display != "standalone" || len(value.Icons) != 4 {
		return false
	}
	seen := make(map[string]bool)
	for _, icon := range value.Icons {
		if !publicIconPath.MatchString(icon.Src) || icon.Type != "image/png" || (icon.Purpose != "any" && icon.Purpose != "maskable") {
			return false
		}
		if icon.Sizes != "192x192" && icon.Sizes != "512x512" {
			return false
		}
		if !strings.HasPrefix(icon.Src, "/assets/beat-"+strings.Split(icon.Sizes, "x")[0]+"-") {
			return false
		}
		seen[icon.Sizes+icon.Purpose] = true
	}
	return len(seen) == 4
}

func cleanManifestTitle(title string) string {
	if !utf8.ValidString(title) {
		return ""
	}
	return strings.TrimSpace(strings.Map(func(value rune) rune {
		if unicode.IsControl(value) || unicode.In(value, unicode.Cf) {
			return -1
		}
		return value
	}, title))
}

func truncateManifestTitle(title string, limit int) string {
	values := []rune(title)
	if len(values) > limit {
		values = values[:limit]
	}
	return string(values)
}
