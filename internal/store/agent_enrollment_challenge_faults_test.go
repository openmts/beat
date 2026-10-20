package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

// enrollmentChallengeSeed 生成一份挑战输入与对应明文，便于消费路径复用。
func enrollmentChallengeSeed(t *testing.T, fixture discoveryFixture, now time.Time) (EnrollmentChallengeInput, []byte) {
	t.Helper()
	raw, err := agentenrollment.GenerateChallenge()
	if err != nil {
		t.Fatal(err)
	}
	return EnrollmentChallengeInput{GrantID: fixture.grant.ID, ChallengeHash: enrollmentDigestBytes(t, raw),
		KeyFingerprint: enrollmentFingerprint(t, fixture.key), MetadataDigest: enrollmentDigest(t, `{"hostname":"edge-01"}`),
		SourceIP: "10.0.0.5", Now: now, ExpiresAt: now.Add(2 * time.Minute)}, raw
}

// seedActiveChallenges 直接落库 maxActiveChallengesPerGrant 条未消费挑战。
func seedActiveChallenges(t *testing.T, fixture discoveryFixture, now time.Time) {
	t.Helper()
	if _, err := fixture.store.db.ExecContext(t.Context(), `WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<?)
		INSERT INTO agent_enrollment_challenges(id, grant_id, challenge_hash, key_fingerprint,
			metadata_digest, source_ip, expires_at, created_at, consumed_at)
		SELECT 'cov-challenge-'||n, ?, randomblob(32), randomblob(32), randomblob(32), '10.0.0.5', ?, ?, NULL FROM seq`,
		maxActiveChallengesPerGrant, fixture.grant.ID, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
}

// TestCreateChallengeFaults 覆盖挑战创建的清理、计数、上限、写入与提交失败。
func TestCreateChallengeFaults(t *testing.T) {
	now := enrollmentFaultNow()
	plainInput := EnrollmentChallengeInput{GrantID: "grant", ChallengeHash: []byte("hash"),
		KeyFingerprint: []byte("fp"), MetadataDigest: []byte("md"), SourceIP: "10.0.0.5",
		Now: now, ExpiresAt: now.Add(time.Minute)}
	t.Run("closed database", func(t *testing.T) {
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		if _, err := closed.CreateChallenge(t.Context(), plainInput); err == nil {
			t.Fatal("closed database created a challenge")
		}
	})
	t.Run("prune blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `INSERT INTO agent_enrollment_challenges(id, grant_id,
			challenge_hash, key_fingerprint, metadata_digest, source_ip, expires_at, created_at, consumed_at)
			VALUES('cov-expired', '`+fixture.grant.ID+`', randomblob(32), randomblob(32), randomblob(32),
				'10.0.0.5', '2026-10-15 11:00:00', '2026-10-15 11:00:00', NULL)`)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_challenge_prune
			BEFORE DELETE ON agent_enrollment_challenges BEGIN SELECT RAISE(ABORT,'prune blocked'); END`)
		input, _ := enrollmentChallengeSeed(t, fixture, now)
		if _, err := fixture.store.CreateChallenge(t.Context(), input); err == nil {
			t.Fatal("blocked challenge prune was accepted")
		}
	})
	t.Run("count unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installChallengeView(t, fixture, `CREATE TRIGGER cov_challenge_view_delete
			INSTEAD OF DELETE ON agent_enrollment_challenges BEGIN SELECT 1; END`)
		input, _ := enrollmentChallengeSeed(t, fixture, now)
		if _, err := fixture.store.CreateChallenge(t.Context(), input); err == nil {
			t.Fatal("unreadable challenge count was accepted")
		}
	})
	t.Run("limit reached", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		seedActiveChallenges(t, fixture, now)
		input, _ := enrollmentChallengeSeed(t, fixture, now)
		if _, err := fixture.store.CreateChallenge(t.Context(), input); !errors.Is(err, ErrEnrollmentChallengeLimit) {
			t.Fatalf("challenge limit error = %v", err)
		}
	})
	t.Run("insert blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_challenge_insert
			BEFORE INSERT ON agent_enrollment_challenges BEGIN SELECT RAISE(ABORT,'insert blocked'); END`)
		input, _ := enrollmentChallengeSeed(t, fixture, now)
		if _, err := fixture.store.CreateChallenge(t.Context(), input); err == nil {
			t.Fatal("blocked challenge insert was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installChallengeCommitFault(t, fixture, "INSERT ON agent_enrollment_challenges")
		input, _ := enrollmentChallengeSeed(t, fixture, now)
		if _, err := fixture.store.CreateChallenge(t.Context(), input); err == nil {
			t.Fatal("deferred challenge commit failure was accepted")
		}
	})
}

// installChallengeView 把挑战表换成同名视图，使清理可执行而计数查询失败。
func installChallengeView(t *testing.T, fixture discoveryFixture, trigger string) {
	t.Helper()
	for _, statement := range []string{
		"ALTER TABLE agent_enrollment_challenges RENAME TO cov_challenges_backup",
		"CREATE TABLE cov_challenges_src AS SELECT * FROM cov_challenges_backup WHERE 0",
		"CREATE VIEW agent_enrollment_challenges AS SELECT * FROM cov_challenges_src",
		trigger,
		"DROP TABLE cov_challenges_src",
	} {
		installEnrollmentTrigger(t, fixture, statement)
	}
}

// installChallengeCommitFault 用延迟外键让挑战写入在提交期失败。
func installChallengeCommitFault(t *testing.T, fixture discoveryFixture, target string) {
	t.Helper()
	installEnrollmentTrigger(t, fixture, `CREATE TABLE cov_challenge_child(
		challenge TEXT REFERENCES agent_enrollment_challenges(id) DEFERRABLE INITIALLY DEFERRED)`)
	installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_challenge_commit AFTER `+target+
		` BEGIN INSERT INTO cov_challenge_child VALUES('missing-challenge'); END`)
}

// TestConsumeChallengeFaults 覆盖挑战消费的打开、读取、更新与提交失败。
func TestConsumeChallengeFaults(t *testing.T) {
	now := enrollmentFaultNow()
	t.Run("closed database", func(t *testing.T) {
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		if _, err := closed.ConsumeChallenge(t.Context(),
			EnrollmentChallengeProof{ID: "missing", Now: now}); err == nil {
			t.Fatal("closed database consumed a challenge")
		}
	})
	t.Run("unknown challenge", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		if _, err := fixture.store.ConsumeChallenge(t.Context(),
			EnrollmentChallengeProof{ID: "missing", Now: now}); !errors.Is(err, ErrEnrollmentChallengeUnavailable) {
			t.Fatalf("unknown challenge error = %v", err)
		}
	})
	t.Run("unscannable row", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		input, _ := enrollmentChallengeSeed(t, fixture, now)
		challenge, err := fixture.store.CreateChallenge(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"UPDATE agent_enrollment_challenges SET expires_at = 'not-a-timestamp' WHERE id = ?", challenge.ID); err != nil {
			t.Fatal(err)
		}
		proof := EnrollmentChallengeProof{ID: challenge.ID, Challenge: []byte("x"),
			KeyFingerprint: input.KeyFingerprint, MetadataDigest: input.MetadataDigest,
			SourceIP: input.SourceIP, Now: now}
		if _, err := fixture.store.ConsumeChallenge(t.Context(), proof); err == nil {
			t.Fatal("unscannable challenge row was consumed")
		}
	})
	t.Run("update blocked", func(t *testing.T) {
		fixture, proof := consumeFixture(t, now)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_challenge_update
			BEFORE UPDATE ON agent_enrollment_challenges BEGIN SELECT RAISE(ABORT,'update blocked'); END`)
		if _, err := fixture.store.ConsumeChallenge(t.Context(), proof); err == nil {
			t.Fatal("blocked challenge update was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		fixture, proof := consumeFixture(t, now)
		installChallengeCommitFault(t, fixture, "UPDATE ON agent_enrollment_challenges")
		if _, err := fixture.store.ConsumeChallenge(t.Context(), proof); err == nil {
			t.Fatal("deferred challenge consume commit failure was accepted")
		}
	})
}

// consumeFixture 落库一条可消费的挑战并返回匹配的证明。
func consumeFixture(t *testing.T, now time.Time) (discoveryFixture, EnrollmentChallengeProof) {
	t.Helper()
	fixture := newEnrollmentFixture(t)
	input, raw := enrollmentChallengeSeed(t, fixture, now)
	challenge, err := fixture.store.CreateChallenge(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, EnrollmentChallengeProof{ID: challenge.ID, Challenge: raw,
		KeyFingerprint: input.KeyFingerprint, MetadataDigest: input.MetadataDigest,
		SourceIP: input.SourceIP, Now: now}
}

// TestChallengeBindingMismatches 覆盖消费证明的四类不匹配判定。
func TestChallengeBindingMismatches(t *testing.T) {
	now := enrollmentFaultNow()
	raw := []byte("cov-challenge")
	digest := agentenrollment.Digest(raw)
	base := EnrollmentChallenge{KeyFingerprint: []byte("fp"), MetadataDigest: []byte("md"),
		SourceIP: "10.0.0.5", ExpiresAt: now.Add(time.Minute)}
	proof := EnrollmentChallengeProof{Challenge: raw, KeyFingerprint: []byte("fp"),
		MetadataDigest: []byte("md"), SourceIP: "10.0.0.5", Now: now}
	if !bindingsMatch(base, digest[:], proof) {
		t.Fatal("valid bindings were rejected")
	}
	expired := base
	expired.ExpiresAt = now.Add(-time.Second)
	if bindingsMatch(expired, digest[:], proof) {
		t.Fatal("expired challenge matched")
	}
	wrongDigest, wrongKey, wrongMetadata := proof, proof, proof
	wrongDigest.Challenge = []byte("other")
	wrongKey.KeyFingerprint = []byte("other")
	wrongMetadata.MetadataDigest = []byte("other")
	for name, candidate := range map[string]EnrollmentChallengeProof{
		"digest": wrongDigest, "key": wrongKey, "metadata": wrongMetadata,
	} {
		t.Run(name, func(t *testing.T) {
			if bindingsMatch(base, digest[:], candidate) {
				t.Fatalf("%s mismatch matched", name)
			}
		})
	}
}
