package agentrollout

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadRechecksFileMetadataAndExpiryDuringRead(t *testing.T) {
	f, input := downloadFixture(t)
	value, err := f.s.OpenDownload(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	if err := os.WriteFile(value.file.Name(), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := value.Read(make([]byte, 1)); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrUnavailable {
		t.Fatal(err)
	}
	f.now = value.ExpiresAt
	if _, err := value.Read(make([]byte, 1)); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestDownloadRevalidatesReadyMetadataPlatformAndPermissions(t *testing.T) {
	f, input := downloadFixture(t)
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.s.directory, manifest.Artifacts[0].SHA256+".artifact")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE agent_release_artifacts SET state='missing'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE nodes SET arch='arm64'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrForbidden {
		t.Fatal(err)
	}
	f.s.enabled = false
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrDisabled {
		t.Fatal(err)
	}
	if _, err := f.s.ListReleases(t.Context(), f.actor, PageQuery{}); err != ErrDisabled {
		t.Fatal(err)
	}
}

func TestMetadataRejectsCorruptOrUnavailableRows(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	if _, err := f.db.DB.Exec("UPDATE agent_releases SET payload=?", []byte("invalid")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReleaseDetail(t.Context(), f.actor, release.ID); err != ErrManifest {
		t.Fatal(err)
	}
	if _, err := f.s.ListReleases(t.Context(), f.actor, PageQuery{}); err != ErrManifest {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE agent_releases SET payload=?", f.signed.Payload); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("DROP TABLE agent_release_artifacts"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReleaseDetail(t.Context(), f.actor, release.ID); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ListRollouts(t.Context(), f.actor, PageQuery{}); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestRepeatedArtifactUploadKeepsOneVerifiedCache(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	for range 2 {
		if err := f.s.PutArtifact(t.Context(), f.actor, ArtifactUpload{ReleaseID: release.ID, ArtifactID: "linux-amd64", Body: bytes.NewReader(f.artifact)}); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(f.s.directory)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
}
