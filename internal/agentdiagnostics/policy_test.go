package agentdiagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

func TestAllowlistAcceptsExactAddressesAndMaskedPrefixes(t *testing.T) {
	set, err := newAllowedSet([]string{"8.8.8.8", "10.0.0.0/8", "2001:db8::/32"})
	if err != nil {
		t.Fatalf("newAllowedSet error = %v", err)
	}
	for _, address := range []string{"8.8.8.8", "::ffff:8.8.8.8", "10.1.2.3", "2001:db8::5"} {
		if !set.allows(parseAddress(t, address)) {
			t.Errorf("allowlist must allow %s", address)
		}
	}
	for _, address := range []string{"8.8.4.4", "11.0.0.1", "2001:db9::1"} {
		if set.allows(parseAddress(t, address)) {
			t.Errorf("allowlist must reject %s", address)
		}
	}
	if _, err := newAllowedSet(nil); !errors.Is(err, ErrPolicy) {
		t.Fatalf("empty allowlist error = %v", err)
	}
	if _, err := newAllowedSet([]string{"example.com"}); !errors.Is(err, ErrPolicy) {
		t.Fatalf("hostname error = %v", err)
	}
	if (allowedSet{}).allows(parseAddress(t, "8.8.8.8")) {
		t.Fatal("empty set must reject every address")
	}
}

func TestAdmissibleTargetIntersectsLocalPolicy(t *testing.T) {
	settings := settings{config: Config{AllowPrivateTargets: false}}
	settings.allowed, _ = newAllowedSet([]string{"10.0.0.0/8", "8.8.8.8", "127.0.0.1"})
	if err := settings.admissibleTarget("8.8.8.8"); err != nil {
		t.Fatalf("public target error = %v", err)
	}
	for _, destination := range []string{"10.0.0.1", "127.0.0.1", "example.com", "0.0.0.0"} {
		if err := settings.admissibleTarget(destination); !errors.Is(err, ErrPolicy) {
			t.Errorf("destination %s error = %v", destination, err)
		}
	}
	settings.config.AllowPrivateTargets = true
	if err := settings.admissibleTarget("10.0.0.1"); err != nil {
		t.Fatalf("private target error = %v", err)
	}
}

func TestJournalRoundTripAndCorruption(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "journal")
	log, err := openJournal(directory)
	if err != nil {
		t.Fatalf("openJournal error = %v", err)
	}
	entry := journalEntry{JobID: "job/1", Nonce: "nonce", Attempt: 3, State: journalStateStarted, Sequence: 12}
	if err := log.save(entry); err != nil {
		t.Fatalf("save error = %v", err)
	}
	loaded, err := log.load("job/1", 3)
	if err != nil || loaded == nil || loaded.Sequence != 12 || loaded.State != journalStateStarted {
		t.Fatalf("load = %+v (%v)", loaded, err)
	}
	if missing, err := log.load("job-2", 1); err != nil || missing != nil {
		t.Fatalf("missing load = %+v (%v)", missing, err)
	}
	if err := log.remove("job/1", 3); err != nil {
		t.Fatalf("remove error = %v", err)
	}
	if err := os.WriteFile(log.path("job-9", 1), []byte("{"), 0o600); err != nil {
		t.Fatalf("write corrupt journal: %v", err)
	}
	if _, err := log.load("job-9", 1); !errors.Is(err, ErrStorage) {
		t.Fatalf("corrupt load error = %v", err)
	}
	if err := log.save(journalEntry{JobID: strings.Repeat("a", 129), Attempt: 1, State: journalStateLeased}); err != nil {
		t.Fatalf("sanitized save error = %v", err)
	}
	pending, err := log.pending()
	if err != nil {
		t.Fatalf("pending error = %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("pending must include the saved entry")
	}
	if sanitizeKey("") != "invalid" || sanitizeKey("../etc/passwd") != "___etc_passwd" {
		t.Fatalf("sanitizeKey results = %q/%q", sanitizeKey(""), sanitizeKey("../etc/passwd"))
	}
}

func TestPrivateFilesRejectUnsafeState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := ensurePrivateDir(directory); err != nil {
		t.Fatalf("ensurePrivateDir error = %v", err)
	}
	target := filepath.Join(directory, "entry.json")
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write loose file: %v", err)
	}
	// 门禁在 umask 077 下运行，必须显式放开权限才能构造出"过宽"的夹具。
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("chmod loose file: %v", err)
	}
	if _, err := readPrivateFile(target); !errors.Is(err, ErrStorage) {
		t.Fatalf("loose file error = %v", err)
	}
	if _, err := readPrivateFile(directory); !errors.Is(err, ErrStorage) {
		t.Fatalf("directory error = %v", err)
	}
	loose := filepath.Join(t.TempDir(), "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatalf("mkdir loose: %v", err)
	}
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatalf("chmod loose: %v", err)
	}
	if err := ensurePrivateDir(loose + "/child"); err != nil {
		t.Fatalf("nested directory error = %v", err)
	}
	if err := ensurePrivateDir(""); !errors.Is(err, ErrStorage) {
		t.Fatalf("empty directory error = %v", err)
	}
	file := filepath.Join(directory, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o700); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := ensurePrivateDir(file); !errors.Is(err, ErrStorage) {
		t.Fatalf("file as directory error = %v", err)
	}
}

func TestErrorCodeCoversEveryDomain(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{nil, ""}, {ErrPolicy, "local_policy"}, {ErrStorage, "storage_failure"}, {ErrExecution, "execution_failed"},
		{ErrCancelled, "canceled"}, {context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"},
		{ErrTransport, "transport_unavailable"}, {errors.New("other"), "diagnostic_failed"},
		{callbackError("observed"), "observed"},
	}
	for _, testCase := range cases {
		if code := ErrorCode(testCase.err); code != testCase.code {
			t.Errorf("ErrorCode(%v) = %q, want %q", testCase.err, code, testCase.code)
		}
	}
	if err := callbackError("observed"); err.Error() != "observed" {
		t.Fatalf("callbackError = %q", err.Error())
	}
}

func TestObserveReportsChangesOnce(t *testing.T) {
	reported := []string{}
	runner := &Runner{onError: func(err error) { reported = append(reported, err.Error()) }}
	runner.observe(ErrTransport)
	runner.observe(ErrTransport)
	runner.observe(nil)
	runner.observe(ErrPolicy)
	if len(reported) != 2 || reported[0] != "transport_unavailable" || reported[1] != "local_policy" {
		t.Fatalf("reported = %+v", reported)
	}
}

func TestValidateLeaseRejectsIncompleteMessages(t *testing.T) {
	runner := diagnosticRunner(t, &fakeGateway{}, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	incomplete := []diagnosticprotocol.Lease{
		{Attempt: 1, Nonce: "nonce", Payload: benchmarkLease("8.8.8.8").Payload},
		{JobID: "job-1", Nonce: "nonce", Payload: benchmarkLease("8.8.8.8").Payload},
		{JobID: "job-1", Attempt: 1, Payload: benchmarkLease("8.8.8.8").Payload},
	}
	for _, lease := range incomplete {
		if _, err := runner.validateLease(lease); !errors.Is(err, ErrPolicy) {
			t.Errorf("lease %+v error = %v", lease, err)
		}
	}
}

func TestRecordRefusesNonceCollision(t *testing.T) {
	gateway := &fakeGateway{}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	lease := benchmarkLease("8.8.8.8")
	if err := runner.record(*lease); err != nil {
		t.Fatalf("record error = %v", err)
	}
	if err := runner.record(*lease); !errors.Is(err, errAlreadyRecorded) {
		t.Fatalf("second record error = %v", err)
	}
	other := *lease
	other.Nonce = "different"
	if err := runner.record(other); !errors.Is(err, ErrPolicy) {
		t.Fatalf("nonce collision error = %v", err)
	}
}

func TestNewRunnerRejectsUnusableStateDirectory(t *testing.T) {
	config := validConfig(t)
	file := filepath.Join(t.TempDir(), "state-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	config.StateDir = file
	if _, err := NewRunner(RunnerConfig{Local: config, Gateway: &fakeGateway{}}); !errors.Is(err, ErrStorage) {
		t.Fatalf("NewRunner error = %v", err)
	}
	invalid := validConfig(t)
	invalid.Maxima.BitrateMbps = 0
	if _, err := NewRunner(RunnerConfig{Local: invalid, Gateway: &fakeGateway{}}); !errors.Is(err, ErrPolicy) {
		t.Fatalf("invalid config error = %v", err)
	}
}

func TestCapabilityStatesReflectConfiguredBinaries(t *testing.T) {
	config := validConfig(t)
	runner := diagnosticRunner(t, &fakeGateway{}, configuredExecutors(config))
	if document := runner.settings.capability(); document.State != capabilityStatePartial {
		t.Fatalf("partial capability = %+v", document)
	}
	config.Iperf3Path = "/usr/bin/iperf3"
	runner = diagnosticRunner(t, &fakeGateway{}, configuredExecutors(config))
	if document := runner.settings.capability(); document.State != capabilityStateReady {
		t.Fatalf("ready capability = %+v", document)
	}
}

func TestBeatReportsTransportErrorsWithoutCancelling(t *testing.T) {
	gateway := &fakeGateway{heartbeatErr: ErrTransport}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	cancelled := false
	if runner.beat(context.Background(), *benchmarkLease("8.8.8.8"), func() { cancelled = true }) {
		t.Fatal("transport failure must not stop the execution loop")
	}
	if cancelled || ErrorCode(nil) != "" {
		t.Fatalf("cancel = %v", cancelled)
	}
}

func TestIperf3ProtocolAndDirectionArguments(t *testing.T) {
	spec := jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolUDP, Port: 5201, DurationSeconds: 3,
		ParallelStreams: 1, BitrateMbps: 5, Direction: model.DiagnosticDirectionReverse}
	arguments, err := iperf3Arguments("/usr/bin/iperf3", spec)
	if err != nil {
		t.Fatalf("iperf3Arguments error = %v", err)
	}
	joined := strings.Join(arguments, " ")
	if !strings.Contains(joined, "-u -R") {
		t.Fatalf("arguments = %q", joined)
	}
	interval := iperf3Interval{BitsPerSec: 8, Bytes: 1, Streams: []iperf3Stream{{Sender: false, BitsPerSec: 16, Bytes: 2}}}
	if samples := intervalSamples(interval, spec); len(samples) != 2 || samples[0].Role != model.DiagnosticRoleReceiver {
		t.Fatalf("interval samples = %+v", samples)
	}
	if samples := throughputSamples(spec, model.DiagnosticRoleSender, -1, 0, interval); samples != nil {
		t.Fatalf("negative throughput samples = %+v", samples)
	}
	summary := iperf3Summary{sent: &iperf3Sum{Bytes: 0}}
	if summary.actualBytes() != 0 {
		t.Fatalf("empty summary bytes = %d", summary.actualBytes())
	}
	if _, err := decodeInterval(nil); !errors.Is(err, ErrExecution) {
		t.Fatalf("decodeInterval error = %v", err)
	}
	if _, err := decodeEnd([]byte("{}")); err != nil {
		t.Fatalf("decodeEnd error = %v", err)
	}
	if _, err := decodeEnd(nil); !errors.Is(err, ErrExecution) {
		t.Fatalf("decodeEnd nil error = %v", err)
	}
}

func TestJournalSaveRejectsOversizedEntries(t *testing.T) {
	log, err := openJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatalf("openJournal error = %v", err)
	}
	entry := journalEntry{JobID: "job", Attempt: 1, State: journalStateLeased, Nonce: strings.Repeat("n", 9000)}
	if err := log.save(entry); !errors.Is(err, ErrStorage) {
		t.Fatalf("oversized entry error = %v", err)
	}
	encoded, err := json.Marshal(entry)
	if err != nil || len(encoded) <= 4096 {
		t.Fatalf("fixture is not oversized (%d, %v)", len(encoded), err)
	}
	if _, err := openJournal(""); !errors.Is(err, ErrStorage) {
		t.Fatalf("empty journal directory error = %v", err)
	}
}

func TestRunnerStatusCountsPartialAsSuccess(t *testing.T) {
	runner := diagnosticRunner(t, &fakeGateway{}, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	runner.count(model.DiagnosticStatePartial)
	runner.count(model.DiagnosticStateUnknown)
	status := runner.Status()
	if status.Completed != 1 || status.Failed != 1 || !status.Enabled {
		t.Fatalf("status = %+v", status)
	}
	runner.enter("job-1")
	if runner.Status().Active != 1 {
		t.Fatal("active jobs must be visible")
	}
	runner.leave("job-1")
	if runner.Status().Active != 0 {
		t.Fatal("finished jobs must leave the active set")
	}
}

func TestPauseHonoursLifetimeCancellation(t *testing.T) {
	runner := diagnosticRunner(t, &fakeGateway{}, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	runner.settings.poll = time.Hour
	if err := runner.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}
	if runner.pause(context.Background()) {
		t.Fatal("pause must return false once the runner is closed")
	}
}

func parseAddress(t *testing.T, value string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatalf("parse %s: %v", value, err)
	}
	return address
}
