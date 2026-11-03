package geoip_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/testutil"
)

func TestGitHubRemoteMMDBReleaseAndInstall(t *testing.T) {
	f := newFixture(t)
	database := testutil.CountryMMDB("JP", 1700000002)
	digest := sha256.Sum256(database)
	releaseBody, err := json.Marshal(map[string]any{
		"tag_name":     "2026.10.22",
		"published_at": "2026-10-22T05:48:01Z",
		"assets": []map[string]any{{
			"name": "GeoLite2-Country.mmdb", "size": len(database), "digest": "sha256:" + hex.EncodeToString(digest[:]),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	geoip.ConfigureTestHTTP(f.service, &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "api.github.com":
			if r.URL.Path != "/repos/P3TERX/GeoLite.mmdb/releases/latest" && r.URL.Path != "/repos/P3TERX/GeoLite.mmdb/releases/tags/2026.10.22" {
				t.Fatalf("unexpected GitHub API path: %s", r.URL.Path)
			}
			return remoteResponse(http.StatusOK, releaseBody), nil
		case "github.com":
			if r.URL.Path != "/P3TERX/GeoLite.mmdb/releases/download/2026.10.22/GeoLite2-Country.mmdb" {
				t.Fatalf("unexpected download path: %s", r.URL.Path)
			}
			return remoteResponse(http.StatusOK, database), nil
		default:
			t.Fatalf("unexpected remote host: %s", r.URL.Host)
			return nil, nil
		}
	})})

	release, err := f.service.RemoteMMDBRelease(t.Context(), f.actor)
	if err != nil || release.Version != "2026.10.22" || !release.UpdateAvailable || release.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("release: %+v %v", release, err)
	}
	settings, err := f.service.InstallRemoteMMDB(t.Context(), f.actor, geoip.RemoteMMDBInstall{
		Version: release.Version, SHA256: release.SHA256, ExpectedRevision: 1,
	})
	if err != nil || settings.MMDB.Source != "github:P3TERX/GeoLite.mmdb@2026.10.22" || settings.MMDB.SHA256 != release.SHA256 {
		t.Fatalf("install: %+v %v", settings, err)
	}
	release, err = f.service.RemoteMMDBRelease(t.Context(), f.actor)
	if err != nil || release.UpdateAvailable {
		t.Fatalf("up-to-date release: %+v %v", release, err)
	}
}

func TestGitHubRemoteMMDBRejectsDigestAndRedirectMismatch(t *testing.T) {
	for _, kind := range []string{"digest", "redirect", "redirect_port"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			database := testutil.CountryMMDB("US", 1700000000)
			digest := sha256.Sum256(database)
			releaseBody, err := json.Marshal(map[string]any{
				"tag_name": "2026.10.22", "published_at": "2026-10-22T05:48:01Z",
				"assets": []map[string]any{{"name": "GeoLite2-Country.mmdb", "size": len(database), "digest": "sha256:" + hex.EncodeToString(digest[:])}},
			})
			if err != nil {
				t.Fatal(err)
			}
			geoip.ConfigureTestHTTP(f.service, &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "api.github.com" {
					return remoteResponse(http.StatusOK, releaseBody), nil
				}
				if kind == "redirect" {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://attacker.invalid/database.mmdb"}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
				}
				if kind == "redirect_port" {
					if r.URL.Host == "github.com:8443" {
						return remoteResponse(http.StatusOK, database), nil
					}
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://github.com:8443/database.mmdb"}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
				}
				return remoteResponse(http.StatusOK, []byte("not a database")), nil
			})})
			_, err = f.service.InstallRemoteMMDB(t.Context(), f.actor, geoip.RemoteMMDBInstall{Version: "2026.10.22", SHA256: hex.EncodeToString(digest[:]), ExpectedRevision: 1})
			if err == nil {
				t.Fatal("remote database mismatch installed")
			}
		})
	}
}

func remoteResponse(status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: make(http.Header)}
}
