package agentrollout

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestOwnerMetadataPaginationAndRollbackDirection(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	ids := f.nodes(t, 3)
	input := CreateInput{Name: "rollback", ReleaseID: release.ID, NodeIDs: ids, Action: protocol.AssignmentRollback}
	if _, err := f.s.Create(t.Context(), f.actor, input); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE nodes SET agent_version='3.0.0'"); err != nil {
		t.Fatal(err)
	}
	input.AcknowledgeRollback = true
	rollout, err := f.s.Create(t.Context(), f.actor, input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.s.ListTargets(t.Context(), f.actor, PageQuery{RolloutID: rollout.ID, Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	next, err := f.s.ListTargets(t.Context(), f.actor, PageQuery{RolloutID: rollout.ID, Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.NextCursor != "" {
		t.Fatal(next, err)
	}
	all, err := f.s.ListRollouts(t.Context(), f.actor, PageQuery{})
	if err != nil || len(all.Items) != 1 || all.Items[0].Action != protocol.AssignmentRollback {
		t.Fatal(all, err)
	}
	var audit int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='rollout.rollback.create'").Scan(&audit); err != nil || audit != 1 {
		t.Fatal(audit, err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	_, targets, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil {
		t.Fatal(err)
	}
	caps := capabilities()
	caps.CurrentVersion = "3.0.0"
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], caps)
	if err != nil || assignment == nil || assignment.Action != protocol.AssignmentRollback {
		t.Fatal(assignment, err)
	}
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReleaseDetail(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetRollout(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Pause(t.Context(), f.actor, rollout.ID); err != ErrRecentAuth {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE admin_users SET role='admin'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ListReleases(t.Context(), f.actor, PageQuery{}); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestReleaseMetadataCursorAndInvalidQueries(t *testing.T) {
	f := newFixture(t)
	f.release(t)
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = "2.1.0"
	f.sign(t, manifest)
	f.release(t)
	first, err := f.s.ListReleases(t.Context(), f.actor, PageQuery{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	second, err := f.s.ListReleases(t.Context(), f.actor, PageQuery{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatal(second, err)
	}
	for _, q := range []PageQuery{{Limit: -1}, {Limit: 101}, {Cursor: "!"}, {Cursor: strings.Repeat("a", 513)}} {
		if _, err := f.s.ListReleases(t.Context(), f.actor, q); err != ErrInvalid {
			t.Fatal(q, err)
		}
	}
	if _, err := f.s.ReleaseDetail(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := f.s.ListTargets(t.Context(), f.actor, PageQuery{RolloutID: "missing"}); err != ErrNotFound {
		t.Fatal(err)
	}
}

func downloadFixture(t *testing.T) (*fixture, DownloadRequest) {
	t.Helper()
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	identity := f.identities[targets[0].NodeID]
	assignment, err := f.s.Lease(t.Context(), identity, capabilities())
	if err != nil || assignment == nil {
		t.Fatal(assignment, err)
	}
	return f, DownloadRequest{Identity: identity, AssignmentID: assignment.ID, ArtifactID: assignment.Artifact.ID, Nonce: assignment.Nonce}
}

func TestDownloadDescriptorBoundsRevocationAndTampering(t *testing.T) {
	f, input := downloadFixture(t)
	download, err := f.s.OpenDownload(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = download.Close() }()
	content, err := io.ReadAll(download)
	if err != nil || !bytes.Equal(content, f.artifact) {
		t.Fatal(string(content), err)
	}
	if _, err := download.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE agent_releases SET revoked_at=?", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := download.Read(make([]byte, 1)); err != ErrConflict {
		t.Fatal("revocation ignored", err)
	}
	if err := download.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE agent_releases SET revoked_at=NULL"); err != nil {
		t.Fatal(err)
	}
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.s.directory, manifest.Artifacts[0].SHA256+".artifact")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), len(f.artifact)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrUnavailable {
		t.Fatal("tamper accepted", err)
	}
	if len(f.s.transfers) != 0 {
		t.Fatal("download slot leaked")
	}
}

func TestDownloadRejectsNonceExpiryIdentityAndCapacity(t *testing.T) {
	f, input := downloadFixture(t)
	changed := input
	changed.Nonce = "short"
	if _, err := f.s.OpenDownload(t.Context(), changed); err != ErrInvalid {
		t.Fatal(err)
	}
	changed = input
	changed.Nonce = strings.Repeat("0", len(input.Nonce))
	if _, err := f.s.OpenDownload(t.Context(), changed); err != ErrForbidden {
		t.Fatal(err)
	}
	changed = input
	changed.ArtifactID = "wrong"
	if _, err := f.s.OpenDownload(t.Context(), changed); err != ErrForbidden {
		t.Fatal(err)
	}
	changed = input
	changed.AssignmentID = "missing"
	if _, err := f.s.OpenDownload(t.Context(), changed); err != ErrNotFound {
		t.Fatal(err)
	}
	downloads := []*Download{}
	t.Cleanup(func() {
		for _, value := range downloads {
			_ = value.Close()
		}
	})
	for range cap(f.s.transfers) {
		value, err := f.s.OpenDownload(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		downloads = append(downloads, value)
	}
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrBusy {
		t.Fatal(err)
	}
	for _, value := range downloads {
		if err := value.Close(); err != nil {
			t.Fatal(err)
		}
	}
	f.expireAssignment(t)
	if _, err := f.s.OpenDownload(t.Context(), input); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestArtifactPublicationDoesNotHoldSQLiteWriterAndRechecksAuthority(t *testing.T) {
	f := newFixture(t)
	release, err := f.s.Import(t.Context(), f.actor, f.signed)
	if err != nil {
		t.Fatal(err)
	}
	body := &authorityReader{Reader: bytes.NewReader(f.artifact), change: func() {
		if _, err := f.db.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
			t.Fatal(err)
		}
	}}
	err = f.s.PutArtifact(t.Context(), f.actor, ArtifactUpload{ReleaseID: release.ID, ArtifactID: "linux-amd64", Body: body})
	if !errors.Is(err, ErrRecentAuth) {
		t.Fatal(err)
	}
	var state string
	if err := f.db.DB.QueryRow("SELECT state FROM agent_release_artifacts WHERE release_id=?", release.ID).Scan(&state); err != nil || state != "missing" {
		t.Fatal(state, err)
	}
	var count int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='release.artifact.import'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.checkArtifact(manifest.Artifacts[0]); err != nil {
		t.Fatal("expected private verified but undispatchable cache", err)
	}
}

type authorityReader struct {
	*bytes.Reader
	change func()
}

func (r *authorityReader) Read(p []byte) (int, error) {
	if r.change != nil {
		r.change()
		r.change = nil
	}
	return r.Reader.Read(p)
}

func TestObservationDoesNotRenewAssignmentDeadline(t *testing.T) {
	f, input := downloadFixture(t)
	var before time.Time
	if err := f.db.DB.QueryRow("SELECT lease_expires_at FROM agent_rollout_targets WHERE current_attempt_id=?", input.AssignmentID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	observation := protocol.Observation{AssignmentID: input.AssignmentID, Nonce: input.Nonce, Version: "2.0.0", ReportAt: f.now, Outcome: "healthy"}
	if err := f.s.Observe(t.Context(), input.Identity, observation); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	if err := f.db.DB.QueryRow("SELECT lease_expires_at FROM agent_rollout_targets WHERE current_attempt_id=?", input.AssignmentID).Scan(&after); err != nil || !after.Equal(before) {
		t.Fatal(before, after, err)
	}
}
