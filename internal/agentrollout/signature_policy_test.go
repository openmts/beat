package agentrollout

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/beat/backend/internal/releasesignature"
)

func (f *fixture) configuredService(t *testing.T, verifier SignatureVerifier) (*Service, error) {
	t.Helper()
	keys := []TrustedKey{}
	for _, key := range f.s.keys {
		keys = append(keys, key)
	}
	return New(Config{Database: f.db.DB, Directory: f.s.directory, Keys: keys, Sigstore: verifier, Enabled: true, ServerSchema: f.s.schema, ProtocolVersion: f.s.protocol, Now: func() time.Time { return f.now }})
}

func TestSignaturePolicyPersistsPublicAnchorsWithoutRuntimePaths(t *testing.T) {
	f := newFixture(t)
	var raw []byte
	if err := f.db.DB.QueryRow("SELECT policy_json FROM agent_release_signature_policies").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var policy releasesignature.PolicyMetadata
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(policy.Root, fixtureSignaturePolicy().Root) || policy.Revision != f.s.signaturePolicy.revision || bytes.Contains(raw, []byte(f.s.directory)) {
		t.Fatal("incorrect public trust snapshot")
	}
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	f.s.sigstore = nil
	if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || output != nil {
		t.Fatal("missing verifier accepted cached release", err)
	}
}

func TestOutOfBandPolicyChangePausesAndRequiresVerifiedReimport(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	next := fixtureSignaturePolicy()
	next.Root = []byte(`{"fixture":"new-offline-root"}`)
	next = sealFixturePolicy(next)
	replacement, err := f.configuredService(t, &fixtureVerifier{policy: &next, bundle: "new-proof"})
	if err != nil {
		t.Fatal(err)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_release_signature_policies", 2)
	f.count(t, "SELECT COUNT(*) FROM agent_releases WHERE verified=0", 1)
	current, _, err := replacement.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || current.State != "paused" || current.Reason != "trust_policy_changed" {
		t.Fatal(current, err)
	}
	if _, err := replacement.Import(t.Context(), f.actor, f.signed); err == nil {
		t.Fatal("old proof accepted under new policy")
	}
	signed := f.signed
	signed.SigstoreBundle = []byte("new-proof")
	if _, err := replacement.Import(t.Context(), f.actor, signed); err != nil {
		t.Fatal(err)
	}
	if err := replacement.Resume(t.Context(), f.actor, ResumeInput{ID: rollout.ID, AcknowledgeUncertainty: true}); err != nil {
		t.Fatal(err)
	}
	if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || output != nil {
		t.Fatal("old runtime policy remained authoritative", err)
	}
	if output, err := replacement.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || output == nil {
		t.Fatal("reverified policy did not admit update", err)
	}
}

func TestArchiveCannotReplaceOutOfBandSignatureRoot(t *testing.T) {
	f := newFixture(t)
	f.release(t)
	f.exec(t, "UPDATE agent_release_signature_policies SET policy_json='{}'")
	if _, err := f.configuredService(t, f.verifier); err == nil {
		t.Fatal("forged archive policy matched runtime anchor")
	}
}

func TestPolicyActivationAuditFailureRollsBackTrustChange(t *testing.T) {
	f := newFixture(t)
	f.release(t)
	original := f.s.signaturePolicy.revision
	next := fixtureSignaturePolicy()
	next.VerifierSHA256 = next.RootSHA256
	next = sealFixturePolicy(next)
	f.exec(t, "CREATE TRIGGER reject_policy BEFORE INSERT ON admin_audit_events WHEN NEW.action='release.signature_policy.loaded' BEGIN SELECT RAISE(ABORT,'full');END")
	if _, err := f.configuredService(t, &fixtureVerifier{policy: &next}); err == nil {
		t.Fatal("unaudited policy activated")
	}
	var current string
	if err := f.db.DB.QueryRow("SELECT policy_revision FROM agent_release_signature_state").Scan(&current); err != nil || current != original {
		t.Fatal(current, err)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_releases WHERE verified=1", 1)
	f.count(t, "SELECT COUNT(*) FROM agent_release_signature_policies", 1)
}

type noPolicyVerifier struct{}

func (noPolicyVerifier) Verify(context.Context, releasesignature.Evidence) error { return nil }

func TestPolicyMetadataValidationAndMissingProviderFailClosed(t *testing.T) {
	if _, err := snapshotSignaturePolicy(noPolicyVerifier{}); err != ErrSignature {
		t.Fatal("unanchored verifier accepted", err)
	}
	for _, change := range []func(*releasesignature.PolicyMetadata){
		func(p *releasesignature.PolicyMetadata) { p.Version = 0 }, func(p *releasesignature.PolicyMetadata) { p.Root = nil },
		func(p *releasesignature.PolicyMetadata) { p.RootSHA256 = "changed" }, func(p *releasesignature.PolicyMetadata) { p.VerifierSHA256 = "invalid" },
		func(p *releasesignature.PolicyMetadata) { p.Revision = "changed" }, func(p *releasesignature.PolicyMetadata) { p.OIDCIssuer = "" },
		func(p *releasesignature.PolicyMetadata) { p.CertificateIdentity = "also-set" }, func(p *releasesignature.PolicyMetadata) { p.OIDCIssuer = "invalid\x00issuer" },
	} {
		policy := fixtureSignaturePolicy()
		change(&policy)
		if err := validateSignaturePolicy(policy); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}

func TestMissingOrMismatchedPolicyLedgerCannotAuthorizeCachedRelease(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE agent_releases SET signature_policy_revision='untrusted'")
	if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || output != nil {
		t.Fatal("mismatched verification policy accepted", err)
	}
	f.exec(t, "DELETE FROM agent_release_signature_state")
	if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
		t.Fatal("missing current trust ledger accepted")
	}
	f.exec(t, "ALTER TABLE agent_release_signature_policies RENAME TO unavailable_policies")
	if _, err := f.configuredService(t, f.verifier); err == nil {
		t.Fatal("missing policy metadata accepted")
	}
}
