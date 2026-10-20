package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

type manifestSettingsStub struct {
	title string
	err   error
}

func (s manifestSettingsStub) Get(context.Context) (model.SiteSettings, error) {
	return model.SiteSettings{SiteTitle: s.title, LogoURL: "https://private.example/logo", SiteDescription: "private description"}, s.err
}

func manifestFixture() publicManifest {
	value := publicManifest{ID: "/", Name: "Beat", ShortName: "Beat", StartURL: "/", Scope: "/", Display: "standalone", Direction: "ltr", Language: "en", Background: "#ffffff", Theme: "#6d28d9"}
	for _, size := range []string{"192", "512"} {
		for _, purpose := range []string{"any", "maskable"} {
			value.Icons = append(value.Icons, publicManifestIcon{Src: "/assets/beat-" + size + "-012345abcdef.png", Sizes: size + "x" + size, Type: "image/png", Purpose: purpose})
		}
	}
	return value
}

func writeManifestFixture(t *testing.T, value publicManifest) string {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.webmanifest"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPublicManifest(t *testing.T) {
	dir := writeManifestFixture(t, manifestFixture())
	mux := http.NewServeMux()
	registerPublicManifest(mux, dir, manifestSettingsStub{title: "\n" + strings.Repeat("界", 100) + "\u202e"})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	var result publicManifest
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || utf8.RuneCountInString(result.Name) != 80 || utf8.RuneCountInString(result.ShortName) != 24 {
		t.Fatalf("unexpected manifest: %d %+v", response.Code, result)
	}
	if response.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(response.Header().Get("Content-Type"), "manifest+json") {
		t.Fatal(response.Header())
	}
	if strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "202e") {
		t.Fatal("manifest disclosed unrelated settings")
	}
	head := httptest.NewRecorder()
	mux.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/manifest.webmanifest", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatal("invalid HEAD response")
	}
}

func TestPublicManifestUnavailable(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  string
		store manifestSettingsStub
	}{
		{name: "missing", store: manifestSettingsStub{title: "Beat"}},
		{name: "malformed", body: "{", store: manifestSettingsStub{title: "Beat"}},
		{name: "oversized", body: strings.Repeat("x", publicManifestLimit+1), store: manifestSettingsStub{title: "Beat"}},
		{name: "invalid UTF8", body: string([]byte{255}), store: manifestSettingsStub{title: "Beat"}},
		{name: "database", store: manifestSettingsStub{title: "Beat", err: errors.New("database down")}},
		{name: "empty title", store: manifestSettingsStub{title: "\n\t"}},
		{name: "invalid title", store: manifestSettingsStub{title: string([]byte{255})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := writeManifestFixture(t, manifestFixture())
			if test.name == "missing" {
				dir = t.TempDir()
			} else if test.body != "" {
				if err := os.WriteFile(filepath.Join(dir, "manifest.webmanifest"), []byte(test.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			mux := http.NewServeMux()
			registerPublicManifest(mux, dir, test.store)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("unexpected status %d", response.Code)
			}
		})
	}
}

func TestPublicManifestValidation(t *testing.T) {
	for _, mutate := range []func(*publicManifest){
		func(v *publicManifest) { v.StartURL = "/admin" }, func(v *publicManifest) { v.Icons = nil },
		func(v *publicManifest) { v.Icons[0].Src = "https://other.example/icon.png" },
		func(v *publicManifest) { v.Icons[0].Purpose = "unknown" }, func(v *publicManifest) { v.Icons[0].Type = "image/svg+xml" },
		func(v *publicManifest) { v.Icons[0].Sizes = "64x64" }, func(v *publicManifest) { v.Icons[0].Sizes = "512x512" },
		func(v *publicManifest) { v.Icons[0] = v.Icons[1] },
	} {
		value := manifestFixture()
		mutate(&value)
		if _, err := readPublicManifest(filepath.Join(writeManifestFixture(t, value), "manifest.webmanifest")); err == nil {
			t.Fatal("accepted invalid manifest")
		}
	}
	if got := cleanManifestTitle(" Beat \n"); got != "Beat" {
		t.Fatal(got)
	}
	if got := truncateManifestTitle("Beat", 24); got != "Beat" {
		t.Fatal(got)
	}
	if _, err := readPublicManifest(t.TempDir()); err == nil {
		t.Fatal("accepted directory")
	}
}
