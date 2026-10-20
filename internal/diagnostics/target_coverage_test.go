//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func coverageTargetInput() model.DiagnosticTargetInput {
	return model.DiagnosticTargetInput{
		Name: "coverage-target", Purpose: "coverage", Kinds: []string{model.DiagnosticKindMTR},
		Address: "93.184.216.34", IPFamily: model.IPFamilyIPv4, Protocol: model.DiagnosticProtocolICMP,
		Direction: model.DiagnosticDirectionForward, MaximumBitrateMbps: 100, MaximumDuration: 10,
		MaximumParallelStream: 1, Enabled: true,
	}
}

func insertCoverageSession(t *testing.T, f *fixture, id, userID string, recent bool) Actor {
	t.Helper()
	until := f.now.Add(-time.Hour)
	if recent {
		until = f.now.Add(time.Hour)
	}
	role := "owner"
	if userID == "admin" {
		role = "admin"
	}
	if _, err := f.database.DB.Exec(`INSERT INTO admin_sessions(id, user_id, token_hash, token_prefix, created_at,
		last_activity_at, idle_expires_at, absolute_expires_at, reauthenticated_until, ip_address, user_agent)
		VALUES(?,?,?,?,?,?,?,?,?, '127.0.0.1','fixture')`, id, userID, "hash-"+id, "prefix"+id, f.now, f.now,
		f.now.Add(time.Hour), f.now.Add(time.Hour), until); err != nil {
		t.Fatal(err)
	}
	return Actor{UserID: userID, Username: userID, Role: role, SessionID: id}
}

func TestSaveTargetRejectsInvalidAndForbiddenInput(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	cases := []struct {
		name   string
		mutate func(*model.DiagnosticTargetInput)
		want   error
	}{
		{"blank name", func(in *model.DiagnosticTargetInput) { in.Name = "  " }, ErrInvalid},
		{"unknown kind", func(in *model.DiagnosticTargetInput) { in.Kinds = []string{"dns"} }, ErrInvalid},
		{"bad bitrate", func(in *model.DiagnosticTargetInput) { in.MaximumBitrateMbps = 0 }, ErrInvalid},
		{"bad allowlist", func(in *model.DiagnosticTargetInput) { in.AllowedAddresses = []string{"not-a-prefix"} }, ErrInvalid},
		{"unresolvable", func(in *model.DiagnosticTargetInput) { in.Address = "missing.example.test" }, ErrInvalid},
		{"loopback", func(in *model.DiagnosticTargetInput) { in.Address = "127.0.0.1" }, ErrForbidden},
		{"policy kind", func(in *model.DiagnosticTargetInput) {
			in.Kinds = []string{model.DiagnosticKindMTR, model.DiagnosticKindIperf3}
		}, ErrForbidden},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := coverageTargetInput()
			test.mutate(&input)
			_, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: input})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTargetWritesRequireOwnerWithRecentAuth(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	admin := insertCoverageSession(t, f, "session-coverage-admin", "admin", true)
	stale := insertCoverageSession(t, f, "session-coverage-stale", "owner", false)
	cases := map[string]Actor{
		"admin":         admin,
		"stale session": stale,
		"unknown":       {UserID: "owner", Role: "owner", SessionID: "session-missing"},
	}
	for name, actor := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.service.SaveTarget(context.Background(), actor, TargetChange{Input: coverageTargetInput()})
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("error = %v, want forbidden", err)
			}
		})
	}
	if _, err := f.service.Policy(context.Background(), actorOf("missing")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("policy actor error = %v", err)
	}
}

func actorOf(session string) Actor {
	return Actor{UserID: "owner", Role: "owner", SessionID: session}
}

func TestTargetUpdateGuardsAndDelete(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)

	input := coverageTargetInput()
	input.Purpose = "changed"
	updated, err := f.service.SaveTarget(context.Background(), f.owner,
		TargetChange{ID: target.ID, ExpectedRevision: target.Revision, Input: input})
	if err != nil || updated.Revision != 2 || updated.Purpose != "changed" {
		t.Fatalf("update = %+v / %v", updated, err)
	}
	if _, err := f.service.SaveTarget(context.Background(), f.owner,
		TargetChange{ID: target.ID, ExpectedRevision: 1, Input: input}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	if _, err := f.service.SaveTarget(context.Background(), f.owner,
		TargetChange{ID: "missing", ExpectedRevision: 1, Input: input}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown target error = %v", err)
	}

	list, err := f.service.ListTargets(context.Background(), f.owner)
	if err != nil || len(list) != 1 || list[0].ResolvedAddresses[0] != "93.184.216.34" {
		t.Fatalf("list = %+v / %v", list, err)
	}
	if _, err := f.service.GetTarget(context.Background(), f.owner, target.ID); err != nil {
		t.Fatalf("get = %v", err)
	}
	if _, err := f.service.GetTarget(context.Background(), f.owner, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get error = %v", err)
	}
	if _, err := f.service.GetTarget(context.Background(), f.owner, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty get error = %v", err)
	}
	// 删除走 tx.one：不存在的目标由影响行数判定为状态冲突。
	if err := f.service.DeleteTarget(context.Background(), f.owner, "missing"); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing delete error = %v", err)
	}
	if err := f.service.DeleteTarget(context.Background(), f.owner, target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.GetTarget(context.Background(), f.owner, target.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted get error = %v", err)
	}
}

func TestDeleteTargetRejectsActiveJobs(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	advertise(t, f)
	createMTRJob(t, f, model.DefaultDiagnosticPolicy(), target)
	if err := f.service.DeleteTarget(context.Background(), f.owner, target.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("active target delete error = %v", err)
	}
}
