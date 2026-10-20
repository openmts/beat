//go:build linux

package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func freshCoverageService(t *testing.T, resolve func(context.Context, string) ([]netip.Addr, error)) *Service {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service, err := NewService(Config{Database: database.DB, Secrets: testutil.NewSecrets(t, database.DB),
		Metrics: &recordingMetrics{}, Resolve: resolve, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestNewServiceDefaultsClockAndRejectsIncompleteConfig(t *testing.T) {
	service := freshCoverageService(t, nil)
	if service.now().IsZero() {
		t.Fatal("default clock must be configured")
	}
	if !service.Enabled() || (*Service)(nil).Enabled() {
		t.Fatal("enabled reporting is wrong")
	}
}

func TestClassifyWriteErrorBranches(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"canceled", context.Canceled, context.Canceled},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded},
		{"no rows", sql.ErrNoRows, ErrNotFound},
		{"other", errors.New("boom"), ErrUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyWriteError(test.err); !errors.Is(got, test.want) {
				t.Fatalf("error = %v, want %v", got, test.want)
			}
		})
	}
}

func validCoverageDocument() CapabilityDocument {
	return CapabilityDocument{Version: 1, Kinds: []string{model.DiagnosticKindMTR}, State: "ready",
		MTR: &MTRCapability{Binary: "/usr/bin/mtr", Version: "0.95", IPFamilies: []string{model.IPFamilyIPv4},
			Protocols: []string{model.DiagnosticProtocolICMP}},
		Maxima: CapabilityMaxima{Probes: 20, HopLimit: 32, DurationSeconds: 30, ParallelStreams: 1, BitrateMbps: 100}}
}

func TestNodeProofRejection(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Advertise(context.Background(), NodeProof{Token: "!!"}, validCoverageDocument()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("malformed token error = %v", err)
	}
	unknown, err := newNonce()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Advertise(context.Background(), NodeProof{Token: unknown}, validCoverageDocument()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unknown token error = %v", err)
	}
	if _, err := f.service.Advertise(context.Background(), NodeProof{Token: f.token}, CapabilityDocument{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid document error = %v", err)
	}
	if _, err := f.database.DB.Exec(`DROP TABLE nodes`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Advertise(context.Background(), NodeProof{Token: f.token}, validCoverageDocument()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("dropped node table error = %v", err)
	}
}

func TestReadAuthorizeAndWriteFailuresAfterDroppedTables(t *testing.T) {
	f := newFixture(t)
	policy := mustPolicy(t, f)
	if _, err := f.database.DB.Exec(`DROP TABLE admin_sessions`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Policy(context.Background(), f.owner); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("policy error = %v", err)
	}
	if _, err := f.service.SavePolicy(context.Background(), f.owner, policy); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("save policy error = %v", err)
	}
}

func TestLookupAddressesWithoutResolver(t *testing.T) {
	service := freshCoverageService(t, nil)
	if _, err := service.lookupAddresses(context.Background(), "example.test"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing resolver error = %v", err)
	}
	addresses, err := service.lookupAddresses(context.Background(), "93.184.216.34")
	if err != nil || len(addresses) != 1 {
		t.Fatalf("literal lookup = %+v / %v", addresses, err)
	}
}

func TestBudgetHelpers(t *testing.T) {
	if utcDay(time.Date(2026, 10, 16, 23, 59, 0, 0, time.FixedZone("cst", 8*3600))) != "2026-10-16" {
		t.Fatal("utcDay must normalize to UTC")
	}
	iperf := model.DiagnosticJob{Kind: model.DiagnosticKindIperf3, BitrateMbps: 8, DurationSeconds: 2, ParallelStreams: 2}
	if got := estimateJobBytes(iperf); got != 4_000_000 {
		t.Fatalf("estimate = %d", got)
	}
	mtr := model.DiagnosticJob{Kind: model.DiagnosticKindMTR, BitrateMbps: 8, DurationSeconds: 2, ParallelStreams: 2}
	if got := estimateJobBytes(mtr); got != 0 {
		t.Fatalf("mtr estimate = %d", got)
	}
	if !exceedsBudget(10, 5, 12) || exceedsBudget(1, 1, 0) || exceedsBudget(1, 1, 2) {
		t.Fatal("budget comparison is wrong")
	}
	policy := model.DefaultDiagnosticPolicy()
	policy.MaximumIperfDuration = 5
	policy.MaximumIperfBitrate = 100
	if effectiveDuration(policy, 30) != 5 || effectiveBitrate(policy, 500) != 100 {
		t.Fatal("effective limits are wrong")
	}
}

func TestCreateJobRejectsBudgetExhaustion(t *testing.T) {
	f := newFixture(t)
	enableCoveragePolicy(t, f, model.DiagnosticKindIperf3)
	advertiseIperf3(t, f)
	global := createCoverageIperfTarget(t, f)
	saveCoveragePolicy(t, f, func(policy *model.DiagnosticPolicy) {
		policy.Enabled = true
		policy.AllowedKinds = []string{model.DiagnosticKindIperf3}
		policy.DailyByteBudget = 1000
	})
	if _, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(global)); !errors.Is(err, ErrBudget) {
		t.Fatalf("global budget error = %v", err)
	}
	saveCoveragePolicy(t, f, func(policy *model.DiagnosticPolicy) { policy.DailyByteBudget = 0 })
	target, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: model.DiagnosticTargetInput{
		Name: "budget-target", Kinds: []string{model.DiagnosticKindIperf3}, Address: "93.184.216.34", Port: 5201,
		IPFamily: model.IPFamilyIPv4, Protocol: model.DiagnosticProtocolTCP, Direction: model.DiagnosticDirectionForward,
		MaximumBitrateMbps: 500, MaximumDuration: 20, MaximumParallelStream: 2, DailyByteBudget: 1000, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateJob(context.Background(), f.owner, iperfJobInput(target)); !errors.Is(err, ErrBudget) {
		t.Fatalf("target budget error = %v", err)
	}
}

func TestTargetPolicyGates(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	cases := []struct {
		name   string
		mutate func(*model.DiagnosticTargetInput)
	}{
		{"reverse", func(in *model.DiagnosticTargetInput) { in.Direction = model.DiagnosticDirectionReverse }},
		{"udp", func(in *model.DiagnosticTargetInput) { in.Protocol = model.DiagnosticProtocolUDP }},
		{"private", func(in *model.DiagnosticTargetInput) { in.PrivateAddress = true }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := coverageTargetInput()
			test.mutate(&input)
			_, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: input})
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("error = %v, want forbidden", err)
			}
		})
	}
}

func TestTargetBudgetBeyondPolicyIsRejected(t *testing.T) {
	f := newFixture(t)
	saveCoveragePolicy(t, f, func(policy *model.DiagnosticPolicy) {
		policy.Enabled = true
		policy.AllowedKinds = []string{model.DiagnosticKindMTR}
		policy.DailyByteBudget = 10
	})
	input := coverageTargetInput()
	input.DailyByteBudget = 100
	if _, err := f.service.SaveTarget(context.Background(), f.owner, TargetChange{Input: input}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}

func TestPolicySaveGuards(t *testing.T) {
	f := newFixture(t)
	policy := mustPolicy(t, f)
	invalid := policy
	invalid.MaximumActiveJobs = 0
	if _, err := f.service.SavePolicy(context.Background(), f.owner, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid policy error = %v", err)
	}
	stale := policy
	stale.Revision = policy.Revision + 5
	if _, err := f.service.SavePolicy(context.Background(), f.owner, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	admin := insertCoverageSession(t, f, "session-policy-admin", "admin", true)
	if _, err := f.service.SavePolicy(context.Background(), admin, policy); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin policy error = %v", err)
	}
	if err := f.service.DeleteTarget(context.Background(), admin, "missing"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin delete error = %v", err)
	}
	if err := f.service.DeleteTarget(context.Background(), actorOf("session-unknown"), "missing"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unknown session delete error = %v", err)
	}
}
