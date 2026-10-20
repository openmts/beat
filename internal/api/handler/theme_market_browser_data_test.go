package handler

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/theme"
)

type browserMarket struct {
	handler                    *ThemeHandler
	service                    *theme.Service
	database                   *store.SQLiteStore
	server                     *httptest.Server
	publicKey, identity        string
	archives                   map[string][]byte
	bodies, signatures         map[int32]string
	version, mode, notModified atomic.Int32
}

func newBrowserMarket(t *testing.T, service *theme.Service, database *store.SQLiteStore) *browserMarket {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	market := &browserMarket{service: service, database: database, publicKey: base64.StdEncoding.EncodeToString(public), archives: map[string][]byte{}, bodies: map[int32]string{}, signatures: map[int32]string{}}
	market.identity, err = theme.MarketIdentity(market.publicKey)
	if err != nil {
		t.Fatal(err)
	}
	market.server = httptest.NewTLSServer(http.HandlerFunc(market.serve))
	t.Cleanup(market.server.Close)
	market.version.Store(1)
	for _, version := range []int32{1, 2} {
		market.prepareVersion(t, version, private)
	}
	client := theme.NewMarketClient(true, time.Now)
	pool := x509.NewCertPool()
	pool.AddCert(market.server.Certificate())
	client.SetRootCAs(pool)
	markets := theme.NewMarketService(store.NewThemeStore(database.DB), client, service, time.Now)
	market.handler = NewThemeHandler(service, markets)
	return market
}

func (market *browserMarket) prepareVersion(t *testing.T, version int32, private ed25519.PrivateKey) {
	t.Helper()
	name := "/v" + strconv.Itoa(int(version)) + ".zip"
	raw := marketBrowserArchive(t, strconv.Itoa(int(version))+".0.0")
	market.archives[name] = raw
	validated, err := theme.ValidateArchive(raw)
	if err != nil {
		t.Fatal(err)
	}
	entry := model.ThemeCatalogEntry{ThemeID: validated.Manifest.ID, Name: validated.Manifest.Name, Version: validated.Manifest.Version, Author: "Beat", Description: "Signed market browser fixture",
		Runtime: validated.Manifest.Runtime, MinimumBeat: validated.Manifest.MinimumBeatVersion, PackageURL: market.server.URL + name,
		PackageSHA256: validated.Digest, ManifestSHA256: sha256HexBytes(validated.ManifestBytes)}
	body, err := json.Marshal(model.ThemeCatalog{SchemaVersion: 1, SourceID: market.identity, ExpiresAt: time.Now().UTC().Add(time.Hour), Themes: []model.ThemeCatalogEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	market.bodies[version], market.signatures[version] = string(body), base64.StdEncoding.EncodeToString(ed25519.Sign(private, body))
}

func (market *browserMarket) serve(w http.ResponseWriter, request *http.Request) {
	if raw, found := market.archives[request.URL.Path]; found {
		_, _ = w.Write(raw)
		return
	}
	if market.mode.Load() == 1 {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	version := market.version.Load()
	etag := `"version-` + strconv.Itoa(int(version)) + `"`
	if request.Header.Get("If-None-Match") == etag && market.mode.Load() == 0 {
		market.notModified.Add(1)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Beat-Catalog-Signature", market.signatures[version])
	if market.mode.Load() == 2 {
		w.Header().Set("X-Beat-Catalog-Signature", "invalid")
	}
	_, _ = io.WriteString(w, market.bodies[version])
}

func (market *browserMarket) setMode(w http.ResponseWriter, request *http.Request) {
	switch request.PathValue("mode") {
	case "v1":
		market.version.Store(1)
		market.mode.Store(0)
	case "v2":
		market.version.Store(2)
		market.mode.Store(0)
	case "offline":
		market.mode.Store(1)
	case "invalid":
		market.mode.Store(2)
	default:
		JSONError(w, 400, "unknown fixture mode")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func marketBrowserArchive(t *testing.T, version string) []byte {
	t.Helper()
	raw, err := theme.BuildArchive(theme.PackOptions{Source: "../../../themes/slate"})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range archive.File {
		copyMarketEntry(t, writer, marketArchiveEntry{file: file, version: version})
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

type marketArchiveEntry struct {
	file    *zip.File
	version string
}

func copyMarketEntry(t *testing.T, writer *zip.Writer, input marketArchiveEntry) {
	t.Helper()
	file, version := input.file, input.version
	reader, err := file.Open()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read fixture archive: %v %v", err, closeErr)
	}
	if file.Name == "beat-theme.json" {
		var manifest model.ThemeManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Version, manifest.Name = version, "Market Slate"
		raw, err = json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
	}
	entry, err := writer.Create(file.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(raw); err != nil {
		t.Fatal(err)
	}
}
