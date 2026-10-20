package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

// grantPolicy 返回一份可用的手动审批策略，供逐字段篡改的分支用例使用。
func grantPolicy(base EnrollmentGrant, name string) agentenrollment.GrantPolicy {
	policy := base.Policy()
	policy.Name = name
	return policy
}

func mustCreateGrant(t *testing.T, fixture discoveryFixture, policy agentenrollment.GrantPolicy) EnrollmentGrant {
	t.Helper()
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate grant secret: %v", err)
	}
	grant, err := fixture.store.CreateGrant(t.Context(),
		EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner-1"}, fixture.now)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	return grant
}

func TestEnrollmentGrantPolicyAndAvailability(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	policy := grantPolicy(fixture.grant, "tagged-fleet")
	policy.Tags = []string{"edge", "prod"}
	policy.SourceCIDRs = []string{"10.0.0.0/8"}
	grant := mustCreateGrant(t, fixture, policy)
	if grant.DefaultGroupName != "enrollment-target" || len(grant.Tags) != 2 ||
		len(grant.SourceCIDRs) != 1 || !grant.Policy().Public == grant.Public {
		t.Fatalf("grant = %+v", grant)
	}
	if !grant.Available(fixture.now) {
		t.Fatal("fresh grant should be available")
	}
	if grant.Available(fixture.now.Add(48 * time.Hour)) {
		t.Fatal("expired grant should not be available")
	}
	revoked, err := fixture.store.RevokeGrant(t.Context(), grant.ID, "owner-1", fixture.now)
	if err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	if revoked.Available(fixture.now) || revoked.Enabled {
		t.Fatalf("revoked grant = %+v", revoked)
	}
	if _, err := fixture.store.RevokeGrant(t.Context(), grant.ID, "owner-1", fixture.now); !errors.Is(err, ErrEnrollmentGrantNotFound) {
		t.Fatalf("second revoke error = %v", err)
	}
	if _, err := fixture.store.RevokeGrant(t.Context(), "missing-grant", "owner-1", fixture.now); !errors.Is(err, ErrEnrollmentGrantNotFound) {
		t.Fatalf("unknown revoke error = %v", err)
	}
}

func TestCreateGrantRejectsInvalidPolicies(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	base := grantPolicy(fixture.grant, "invalid-fleet")
	tooManyCIDRs := make([]string, agentenrollment.MaxGrantSourceCIDRs+1)
	for index := range tooManyCIDRs {
		tooManyCIDRs[index] = "10.0.0.0/8"
	}
	cases := map[string]func(*agentenrollment.GrantPolicy){
		"blank name":           func(policy *agentenrollment.GrantPolicy) { policy.Name = "   " },
		"control characters":   func(policy *agentenrollment.GrantPolicy) { policy.Name = "edge\x01fleet" },
		"unknown mode":         func(policy *agentenrollment.GrantPolicy) { policy.Mode = "sometimes" },
		"missing group":        func(policy *agentenrollment.GrantPolicy) { policy.DefaultGroupID = "" },
		"capacity zero":        func(policy *agentenrollment.GrantPolicy) { policy.MaximumEnrollments = 0 },
		"expiry in past":       func(policy *agentenrollment.GrantPolicy) { policy.ExpiresAt = fixture.now.Add(-time.Hour) },
		"expiry too far":       func(policy *agentenrollment.GrantPolicy) { policy.ExpiresAt = fixture.now.AddDate(20, 0, 0) },
		"ssh port zero":        func(policy *agentenrollment.GrantPolicy) { policy.SSHPort = 0 },
		"invalid tag":          func(policy *agentenrollment.GrantPolicy) { policy.Tags = []string{"bad,tag"} },
		"too many source cidr": func(policy *agentenrollment.GrantPolicy) { policy.SourceCIDRs = tooManyCIDRs },
		"bad hostname regex":   func(policy *agentenrollment.GrantPolicy) { policy.HostnamePattern = "([" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			policy := base
			mutate(&policy)
			secret, err := agentenrollment.GenerateGrantSecret()
			if err != nil {
				t.Fatalf("generate secret: %v", err)
			}
			if _, err := fixture.store.CreateGrant(t.Context(),
				EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner-1"}, fixture.now); err == nil {
				t.Fatal("invalid policy was accepted")
			}
		})
	}
	policy := base
	policy.Mode = agentenrollment.ApprovalAutomatic
	policy.NodeNamePrefix = ""
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	if _, err := fixture.store.CreateGrant(t.Context(),
		EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner-1"}, fixture.now); !errors.Is(err, agentenrollment.ErrInvalidGrantPolicy) {
		t.Fatalf("automatic grant without prefix error = %v", err)
	}
}

func TestCreateGrantRejectsDuplicateName(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	policy := grantPolicy(fixture.grant, "duplicate-fleet")
	mustCreateGrant(t, fixture, policy)
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	if _, err := fixture.store.CreateGrant(t.Context(),
		EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner-1"}, fixture.now); !errors.Is(err, ErrEnrollmentGrantNameTaken) {
		t.Fatalf("duplicate name error = %v", err)
	}
}

func TestEnrollmentGrantLookups(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	grants, err := fixture.store.ListGrants(t.Context())
	if err != nil || len(grants) != 1 || grants[0].ID != fixture.grant.ID {
		t.Fatalf("list grants = %+v err = %v", grants, err)
	}
	if _, err := fixture.store.Grant(t.Context(), "missing-grant"); !errors.Is(err, ErrEnrollmentGrantNotFound) {
		t.Fatalf("unknown grant error = %v", err)
	}
	found, err := fixture.store.GrantBySecret(t.Context(), fixture.secret.Plaintext, fixture.now)
	if err != nil || found.ID != fixture.grant.ID {
		t.Fatalf("grant by secret = %+v err = %v", found, err)
	}
	if _, err := fixture.store.GrantBySecret(t.Context(), "not-the-secret", fixture.now); !errors.Is(err, ErrEnrollmentGrantUnavailable) {
		t.Fatalf("unknown secret error = %v", err)
	}
	disabledPolicy := grantPolicy(fixture.grant, "disabled-fleet")
	disabledPolicy.Enabled = false
	disabled := mustCreateGrant(t, fixture, disabledPolicy)
	if disabled.Available(fixture.now) {
		t.Fatal("disabled grant reported available")
	}
	exhaustedPolicy := grantPolicy(fixture.grant, "exhausted-fleet")
	exhaustedPolicy.MaximumEnrollments = 5
	exhausted := mustCreateGrant(t, fixture, exhaustedPolicy)
	if _, err := fixture.store.db.ExecContext(t.Context(),
		"UPDATE agent_enrollment_grants SET consumed_enrollments = maximum_enrollments WHERE id = ?", exhausted.ID); err != nil {
		t.Fatalf("exhaust grant: %v", err)
	}
	reloaded, err := fixture.store.Grant(t.Context(), exhausted.ID)
	if err != nil || reloaded.Available(fixture.now) {
		t.Fatalf("exhausted grant = %+v err = %v", reloaded, err)
	}
}

func TestRotateGrantSecretInvalidatesChallenges(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	challengeBytes, err := agentenrollment.GenerateChallenge()
	if err != nil {
		t.Fatalf("generate challenge: %v", err)
	}
	fingerprint := enrollmentFingerprint(t, fixture.key)
	if _, err := fixture.store.CreateChallenge(t.Context(), EnrollmentChallengeInput{
		GrantID: fixture.grant.ID, ChallengeHash: enrollmentDigestBytes(t, challengeBytes),
		KeyFingerprint: fingerprint, MetadataDigest: enrollmentDigest(t, "rotate"),
		SourceIP: "10.0.0.5", Now: fixture.now, ExpiresAt: fixture.now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	rotated, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate rotated secret: %v", err)
	}
	updated, err := fixture.store.RotateGrantSecret(t.Context(), fixture.grant.ID, rotated, fixture.now.Add(time.Minute))
	if err != nil || updated.SecretPrefix != rotated.Prefix {
		t.Fatalf("rotate = %+v err = %v", updated, err)
	}
	if _, err := fixture.store.CreateChallenge(t.Context(), EnrollmentChallengeInput{
		GrantID: fixture.grant.ID, ChallengeHash: enrollmentDigestBytes(t, challengeBytes),
		KeyFingerprint: fingerprint, MetadataDigest: enrollmentDigest(t, "rotate"),
		SourceIP: "10.0.0.5", Now: fixture.now, ExpiresAt: fixture.now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("recreate challenge: %v", err)
	}
	if _, err := fixture.store.GrantBySecret(t.Context(), fixture.secret.Plaintext, fixture.now); !errors.Is(err, ErrEnrollmentGrantUnavailable) {
		t.Fatalf("stale secret error = %v", err)
	}
	if _, err := fixture.store.GrantBySecret(t.Context(), rotated.Plaintext, fixture.now); err != nil {
		t.Fatalf("rotated secret error = %v", err)
	}
	if _, err := fixture.store.RotateGrantSecret(t.Context(), "missing-grant", rotated, fixture.now); !errors.Is(err, ErrEnrollmentGrantNotFound) {
		t.Fatalf("unknown rotate error = %v", err)
	}
}

func TestUpdateGrantValidationAndCapacity(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	updatedPolicy := grantPolicy(fixture.grant, "renamed-fleet")
	updatedPolicy.MaximumEnrollments = 5
	updatedPolicy.SSHPort = 2222
	updatedPolicy.Mode = agentenrollment.ApprovalAutomatic
	updatedPolicy.NodeNamePrefix = "edge"
	updated, err := fixture.store.UpdateGrant(t.Context(), fixture.grant.ID,
		EnrollmentGrantInput{Policy: updatedPolicy, ActorID: "owner-1"}, fixture.now.Add(time.Minute))
	if err != nil || updated.Name != "renamed-fleet" || updated.SSHPort != 2222 {
		t.Fatalf("update = %+v err = %v", updated, err)
	}
	if _, err := fixture.store.UpdateGrant(t.Context(), "missing-grant",
		EnrollmentGrantInput{Policy: updatedPolicy}, fixture.now); !errors.Is(err, ErrEnrollmentGrantNotFound) {
		t.Fatalf("unknown update error = %v", err)
	}
	invalid := updatedPolicy
	invalid.SSHPort = 70000
	if _, err := fixture.store.UpdateGrant(t.Context(), fixture.grant.ID,
		EnrollmentGrantInput{Policy: invalid}, fixture.now); !errors.Is(err, agentenrollment.ErrInvalidGrantPolicy) {
		t.Fatalf("invalid update error = %v", err)
	}
	if _, err := fixture.store.db.ExecContext(t.Context(),
		"UPDATE agent_enrollment_grants SET consumed_enrollments = 3 WHERE id = ?", fixture.grant.ID); err != nil {
		t.Fatalf("consume capacity: %v", err)
	}
	// 被更新的授权上限必须足够大，否则库层 CHECK 会先于容量分支失败。
	shrunk := grantPolicy(fixture.grant, "renamed-fleet")
	shrunk.MaximumEnrollments = 2
	if _, err := fixture.store.UpdateGrant(t.Context(), fixture.grant.ID,
		EnrollmentGrantInput{Policy: shrunk}, fixture.now); !errors.Is(err, ErrEnrollmentGrantCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
	other := mustCreateGrant(t, fixture, grantPolicy(fixture.grant, "other-fleet"))
	clashing := grantPolicy(fixture.grant, other.Name)
	clashing.MaximumEnrollments = 5
	if _, err := fixture.store.UpdateGrant(t.Context(), fixture.grant.ID,
		EnrollmentGrantInput{Policy: clashing}, fixture.now); !errors.Is(err, ErrEnrollmentGrantNameTaken) {
		t.Fatalf("name clash error = %v", err)
	}
}

func TestGrantListEncodingHelpers(t *testing.T) {
	if encoded := encodeGrantList(nil); encoded != "[]" {
		t.Fatalf("nil list = %s", encoded)
	}
	if decoded := decodeGrantList("not json"); len(decoded) != 0 {
		t.Fatalf("broken list = %v", decoded)
	}
	if decoded := decodeGrantList(`["edge"]`); len(decoded) != 1 || decoded[0] != "edge" {
		t.Fatalf("list = %v", decoded)
	}
	record := enrollmentGrantRecord{Policy: agentenrollment.GrantPolicy{Tags: []string{"edge"},
		SourceCIDRs: []string{"10.0.0.0/8"}}}
	if !strings.Contains(record.tagsJSON(), "edge") || !strings.Contains(record.cidrJSON(), "10.0.0.0/8") {
		t.Fatalf("record json = %s %s", record.tagsJSON(), record.cidrJSON())
	}
}
