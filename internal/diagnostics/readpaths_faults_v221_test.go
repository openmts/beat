//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// execSQL 直接对夹具数据库执行 DDL/DML，用于注入存储层故障。
func execSQL(t *testing.T, f *fixture, query string, args ...any) {
	t.Helper()
	if _, err := f.database.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func dropTable(t *testing.T, f *fixture, name string) {
	t.Helper()
	execSQL(t, f, "DROP TABLE "+name)
}

func TestAdminReadPathsFailClosed(t *testing.T) {
	f := newFixture(t)
	intruder := Actor{UserID: "intruder", Role: "admin", SessionID: "session-intruder"}
	cases := []struct {
		name string
		want error
		call func() error
	}{
		{"list jobs", ErrForbidden, func() error { _, err := f.service.ListJobs(t.Context(), intruder, JobFilter{}); return err }},
		{"job detail", ErrForbidden, func() error { _, err := f.service.Job(t.Context(), intruder, "job"); return err }},
		{"job detail without id", ErrInvalid, func() error { _, err := f.service.Job(t.Context(), f.owner, ""); return err }},
		{"list targets", ErrForbidden, func() error { _, err := f.service.ListTargets(t.Context(), intruder); return err }},
		{"get target", ErrForbidden, func() error { _, err := f.service.GetTarget(t.Context(), intruder, "target"); return err }},
		{"policy", ErrForbidden, func() error { _, err := f.service.Policy(t.Context(), intruder); return err }},
		{"results", ErrForbidden, func() error { _, err := f.service.Results(t.Context(), intruder, ResultQuery{}); return err }},
		{"capabilities", ErrForbidden, func() error { _, err := f.service.Capabilities(t.Context(), intruder); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestJobReadsFailClosedWhenStorageIsUnavailable(t *testing.T) {
	f := newFixture(t)
	dropTable(t, f, "diagnostic_jobs")
	if _, err := f.service.ListJobs(t.Context(), f.owner, JobFilter{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("list jobs error = %v", err)
	}
	if _, err := f.service.Job(t.Context(), f.owner, "job"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("job detail error = %v", err)
	}
	if _, err := f.service.Results(t.Context(), f.owner, ResultQuery{JobID: "job"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("results error = %v", err)
	}
}

func TestTargetReadsFailClosedWhenStorageIsUnavailable(t *testing.T) {
	f := newFixture(t)
	dropTable(t, f, "diagnostic_targets")
	if _, err := f.service.ListTargets(t.Context(), f.owner); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("list targets error = %v", err)
	}
	if _, err := f.service.GetTarget(t.Context(), f.owner, "target"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("get target error = %v", err)
	}
}

func TestHopIdentityReadsFailClosed(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "public-mtr", "93.184.216.34", false)
	advertise(t, f)
	job := createMTRJob(t, f, model.DiagnosticPolicy{}, target)
	dropTable(t, f, "diagnostic_hop_identities")
	if _, err := f.service.Job(t.Context(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("job detail error = %v", err)
	}
}

func TestPolicyReadsFailClosed(t *testing.T) {
	t.Run("missing table", func(t *testing.T) {
		f := newFixture(t)
		dropTable(t, f, "diagnostic_policies")
		if _, err := f.service.Policy(t.Context(), f.owner); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("policy error = %v", err)
		}
	})
	t.Run("invalid kinds payload", func(t *testing.T) {
		f := newFixture(t)
		execSQL(t, f, `INSERT INTO diagnostic_policies(id, enabled, allowed_kinds_json, updated_at)
			VALUES('default', 0, 'not-json', ?)`, time.Now().UTC())
		if _, err := f.service.Policy(t.Context(), f.owner); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("policy error = %v", err)
		}
	})
	t.Run("invalid policy payload", func(t *testing.T) {
		f := newFixture(t)
		execSQL(t, f, `INSERT INTO diagnostic_policies(id, enabled, allowed_kinds_json, updated_at)
			VALUES('default', 0, '[]', ?)`, time.Now().UTC())
		if _, err := f.service.Policy(t.Context(), f.owner); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("policy error = %v", err)
		}
	})
}

func TestSavePolicyFailureModes(t *testing.T) {
	t.Run("policy storage unavailable", func(t *testing.T) {
		f := newFixture(t)
		policy := mustPolicy(t, f)
		dropTable(t, f, "diagnostic_policies")
		if _, err := f.service.SavePolicy(t.Context(), f.owner, policy); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("save policy error = %v", err)
		}
	})
	t.Run("policy write rejected", func(t *testing.T) {
		f := newFixture(t)
		saved, err := f.service.SavePolicy(context.Background(), f.owner, mustPolicy(t, f))
		if err != nil {
			t.Fatal(err)
		}
		execSQL(t, f, `CREATE TRIGGER v221_policy_write BEFORE UPDATE ON diagnostic_policies WHEN 1
			BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if _, err = f.service.SavePolicy(t.Context(), f.owner, saved); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("save policy error = %v", err)
		}
	})
	t.Run("policy audit rejected", func(t *testing.T) {
		f := newFixture(t)
		execSQL(t, f, `CREATE TRIGGER v221_policy_audit BEFORE INSERT ON admin_audit_events
			WHEN NEW.action='diagnostic.policy.updated' BEGIN SELECT RAISE(ABORT, 'v221'); END`)
		if _, err := f.service.SavePolicy(t.Context(), f.owner, mustPolicy(t, f)); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("save policy error = %v", err)
		}
	})
}

func TestCapabilityReadsFailClosed(t *testing.T) {
	f := newFixture(t)
	dropTable(t, f, "diagnostic_capabilities")
	if _, err := f.service.Capabilities(t.Context(), f.owner); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("capabilities error = %v", err)
	}
	if _, err := f.service.Advertise(t.Context(), NodeProof{Token: f.token}, CapabilityDocument{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty advertise error = %v", err)
	}
}
