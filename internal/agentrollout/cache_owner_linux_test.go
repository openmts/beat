//go:build linux

package agentrollout

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateCacheRejectsForeignOwners(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership fixture requires isolated root test process")
	}
	parent := t.TempDir()
	foreign := filepath.Join(parent, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(foreign, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := privateReleaseDirectory(foreign); err == nil {
		t.Fatal("foreign-owned cache accepted")
	}
	if err := secureReleaseParents(filepath.Join(foreign, "cache")); err == nil {
		t.Fatal("foreign-owned ancestor accepted")
	}
	f := newFixture(t)
	release := f.release(t)
	artifact := release.Manifest.Artifacts[0]
	path := filepath.Join(f.s.directory, artifact.SHA256+".artifact")
	if err := os.Chown(path, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := f.s.checkArtifact(artifact); err == nil {
		t.Fatal("foreign-owned artifact accepted")
	}
}
