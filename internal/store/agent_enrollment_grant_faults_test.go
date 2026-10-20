package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/agentenrollment"
)

// createGrantWithSecret 创建一条授权并返回明文秘密，供按秘密查找的用例使用。
func createGrantWithSecret(t *testing.T, fixture discoveryFixture, policy agentenrollment.GrantPolicy) (EnrollmentGrant, agentenrollment.Secret) {
	t.Helper()
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatal(err)
	}
	grant, err := fixture.store.CreateGrant(t.Context(),
		EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner-1"}, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	return grant, secret
}

// corruptGrantTimestamp 把授权的时间列写成不可解析值，触发行扫描失败。
func corruptGrantTimestamp(t *testing.T, fixture discoveryFixture) {
	t.Helper()
	installEnrollmentTrigger(t, fixture, "UPDATE agent_enrollment_grants SET expires_at = 'not-a-timestamp'")
}

// TestCreateGrantFaults 覆盖授权创建的事务打开、提交与回读失败。
func TestCreateGrantFaults(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		fixture := discoveryFixture{grant: EnrollmentGrant{ID: "grant"}}
		input := EnrollmentGrantInput{Policy: grantPolicy(fixture.grant, "cov-closed"),
			Secret: agentenrollment.Secret{}, ActorID: "owner-1"}
		input.Policy.Enabled = true
		if _, err := closed.CreateGrant(t.Context(), input, enrollmentFaultNow()); err == nil {
			t.Fatal("closed database created a grant")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `CREATE TABLE cov_grant_child(
			grant TEXT REFERENCES agent_enrollment_grants(id) DEFERRABLE INITIALLY DEFERRED)`)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_grant_commit AFTER INSERT ON agent_enrollment_grants
			BEGIN INSERT INTO cov_grant_child VALUES('missing-grant'); END`)
		secret, err := agentenrollment.GenerateGrantSecret()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.CreateGrant(t.Context(), EnrollmentGrantInput{
			Policy: grantPolicy(fixture.grant, "cov-commit"), Secret: secret,
			ActorID: "owner-1"}, fixture.now); err == nil {
			t.Fatal("deferred grant commit failure was accepted")
		}
	})
	t.Run("reread failed", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, "ALTER TABLE groups RENAME TO cov_groups_backup")
		secret, err := agentenrollment.GenerateGrantSecret()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.CreateGrant(t.Context(), EnrollmentGrantInput{
			Policy: grantPolicy(fixture.grant, "cov-reread"), Secret: secret,
			ActorID: "owner-1"}, fixture.now); err == nil {
			t.Fatal("grant reread failure was accepted")
		}
	})
}

// TestListGrantFaults 覆盖授权清单的行扫描失败。
func TestListGrantFaults(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	corruptGrantTimestamp(t, fixture)
	if _, err := fixture.store.ListGrants(t.Context()); err == nil {
		t.Fatal("unscannable grant row was accepted")
	}
}

// TestGrantSecretLookupFaults 覆盖按秘密查找的不可用判定与行扫描失败。
func TestGrantSecretLookupFaults(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		grant, secret := createGrantWithSecret(t, fixture, grantPolicy(fixture.grant, "cov-revoked"))
		if _, err := fixture.store.RevokeGrant(t.Context(), grant.ID, "owner-1", fixture.now); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.GrantBySecret(t.Context(), secret.Plaintext, fixture.now); !errors.Is(err, ErrEnrollmentGrantUnavailable) {
			t.Fatalf("revoked grant lookup error = %v", err)
		}
	})
	t.Run("scan failure", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		_, secret := createGrantWithSecret(t, fixture, grantPolicy(fixture.grant, "cov-scan"))
		corruptGrantTimestamp(t, fixture)
		if _, err := fixture.store.GrantBySecret(t.Context(), secret.Plaintext, fixture.now); err == nil {
			t.Fatal("unscannable grant row was accepted")
		}
	})
}

// TestGrantMutationFaults 覆盖授权更新的事务打开、副作用、审计、提交与回读失败。
func TestGrantMutationFaults(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		if _, err := closed.RevokeGrant(t.Context(), "missing", "owner-1", enrollmentFaultNow()); err == nil {
			t.Fatal("closed database revoked a grant")
		}
	})
	t.Run("side effect blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		input, _ := enrollmentChallengeSeed(t, fixture, fixture.now)
		if _, err := fixture.store.CreateChallenge(t.Context(), input); err != nil {
			t.Fatal(err)
		}
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_revoke_challenges
			BEFORE DELETE ON agent_enrollment_challenges BEGIN SELECT RAISE(ABORT,'delete blocked'); END`)
		if _, err := fixture.store.RevokeGrant(t.Context(), fixture.grant.ID, "owner-1", fixture.now); err == nil {
			t.Fatal("blocked revoke side effect was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_grant_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		secret, err := agentenrollment.GenerateGrantSecret()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.RotateGrantSecret(t.Context(), fixture.grant.ID, secret, fixture.now); err == nil {
			t.Fatal("blocked grant audit was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `CREATE TABLE cov_grant_update_child(
			grant TEXT REFERENCES agent_enrollment_grants(id) DEFERRABLE INITIALLY DEFERRED)`)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_grant_update_commit
			AFTER UPDATE ON agent_enrollment_grants BEGIN INSERT INTO cov_grant_update_child VALUES('missing'); END`)
		secret, err := agentenrollment.GenerateGrantSecret()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.RotateGrantSecret(t.Context(), fixture.grant.ID, secret, fixture.now); err == nil {
			t.Fatal("deferred grant update commit failure was accepted")
		}
	})
	t.Run("reread failed", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, "ALTER TABLE groups RENAME TO cov_groups_backup")
		secret, err := agentenrollment.GenerateGrantSecret()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.RotateGrantSecret(t.Context(), fixture.grant.ID, secret, fixture.now); err == nil {
			t.Fatal("grant mutation reread failure was accepted")
		}
	})
}

// TestGrantWriteErrorMapping 覆盖授权写入错误的三个归一分支。
func TestGrantWriteErrorMapping(t *testing.T) {
	cases := map[string]struct {
		message string
		want    error
	}{
		"name taken":   {"UNIQUE constraint failed: agent_enrollment_grants.name", ErrEnrollmentGrantNameTaken},
		"secret clash": {"UNIQUE constraint failed: agent_enrollment_grants.secret_hash", ErrEnrollmentGrantSecretConflict},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			mapped := mapEnrollmentGrantWriteError("create enrollment grant", errors.New(tt.message))
			if !errors.Is(mapped, tt.want) {
				t.Fatalf("mapped error = %v", mapped)
			}
		})
	}
	t.Run("other", func(t *testing.T) {
		source := errors.New("disk unavailable")
		if mapped := mapEnrollmentGrantWriteError("create enrollment grant", source); !errors.Is(mapped, source) {
			t.Fatalf("mapped error = %v", mapped)
		}
	})
}
