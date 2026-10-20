package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	_ "unsafe" // 仅测试连接固定版本 MTS 的真实文件系统故障入口。

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

type mtsFilesystemFault interface{ Before(string) error }

//go:linkname installMTSFilesystemFault github.com/openmts/mts/internal/storagefs.SetFaultController
func installMTSFilesystemFault(mtsFilesystemFault) func()

type agentWALFault struct {
	mu                 sync.Mutex
	stage              string
	walWritten, failed bool
}

func (fault *agentWALFault) Before(operation string) error {
	fault.mu.Lock()
	defer fault.mu.Unlock()
	if fault.stage == "sync" && fault.walWritten && !fault.failed && operation == "sync" {
		fault.failed = true
		return errors.New("injected durable WAL sync failure")
	}
	return nil
}

func (fault *agentWALFault) BeforeWrite(file *os.File, data []byte) (int, bool, error) {
	fault.mu.Lock()
	defer fault.mu.Unlock()
	if !strings.Contains(filepath.ToSlash(file.Name()), "/wal/") || fault.failed {
		return 0, false, nil
	}
	fault.walWritten = true
	if fault.stage == "sync" {
		return 0, false, nil
	}
	fault.failed = true
	if fault.stage == "before" {
		return 0, true, errors.New("injected WAL write failure")
	}
	written, err := file.Write(data[:len(data)/2])
	return written, true, errors.Join(err, errors.New("injected short WAL write"))
}

func TestMTSAgentDurableWALChild(t *testing.T) {
	directory := os.Getenv("BEAT_TEST_AGENT_WAL_DIR")
	if directory == "" {
		t.Skip("child process only")
	}
	metrics, err := NewMTSStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	input := ingestInput(time.Unix(1_800_000_000, 100).UTC(), 100)
	input.Report.SampleID = "d6d7a562-05aa-440b-a9e0-3bddfba76dce"
	input.Report.CustomMetrics = map[string]float64{"queue": 7}
	input.Report.CustomSamples = map[string]model.CustomSampleMetadata{"queue": testutil.CustomSourceFixture(input.Report.CapturedAt, model.CustomMetric{ID: "fixture", Name: "queue", ShellCommand: "printf 7", IntervalSeconds: 5})}
	metadata := input.Report.CustomSamples["queue"]
	metadata.SampleID = "e6d7a562-05aa-440b-a9e0-3bddfba76dcf"
	input.Report.CustomSamples["queue"] = metadata
	acceptWALIngest(t, metrics, input)
	fault := &agentWALFault{stage: os.Getenv("BEAT_TEST_AGENT_WAL_FAULT")}
	if fault.stage != "" {
		restore := installMTSFilesystemFault(fault)
		input.Report.SampleID = "d6d7a562-05aa-440b-a9e0-3bddfba76dcf"
		input.Report.CapturedAt = input.Report.CapturedAt.Add(time.Second)
		input.Report.Metrics = &model.NodeMetrics{CPU: 50, NetRecvTotal: 160, NetSentTotal: 80}
		attempt, prepareErr := metrics.PrepareAgentReport(t.Context(), input, walIngestionPolicy)
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		_, writeErr := attempt.Commit(t.Context())
		attempt.Close()
		restore()
		if !errors.Is(writeErr, ErrMTSRecoveryRequired) || !fault.failed {
			t.Fatal("real WAL failure was not fenced", writeErr)
		}
		if healthy, _ := metrics.Health(); healthy {
			t.Fatal("poisoned engine advertised healthy")
		}
		if err := metrics.WriteMetric(t.Context(), "node", "cpu", 99, input.Report.CapturedAt.Add(time.Second)); !errors.Is(err, ErrMTSRecoveryRequired) {
			t.Fatal("new write passed poisoned boundary", err)
		}
	}
	// 不 Close/Flush：父进程必须只靠 Sync WAL 恢复已确认数据。
	os.Exit(0)
}

func TestMTSAgentWALRestartNeverLosesAcknowledgedSample(t *testing.T) {
	for _, stage := range []string{"", "before", "sync", "short"} {
		t.Run("fault_"+stage, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "metrics")
			child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMTSAgentDurableWALChild$")
			child.Env = append(os.Environ(), "BEAT_TEST_AGENT_WAL_DIR="+directory, "BEAT_TEST_AGENT_WAL_FAULT="+stage)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("WAL child: %s %v", output, err)
			}
			metrics, err := NewMTSStore(directory)
			if err != nil {
				t.Fatal("recover WAL", err)
			}
			t.Cleanup(func() {
				if err := metrics.Close(); err != nil {
					t.Error(err)
				}
			})
			at := time.Unix(1_800_000_000, 100).UTC()
			input := ingestInput(at, 100)
			input.Report.SampleID = "d6d7a562-05aa-440b-a9e0-3bddfba76dce"
			input.Report.CustomMetrics = map[string]float64{"queue": 7}
			input.Report.CustomSamples = map[string]model.CustomSampleMetadata{"queue": testutil.CustomSourceFixture(input.Report.CapturedAt, model.CustomMetric{ID: "fixture", Name: "queue", ShellCommand: "printf 7", IntervalSeconds: 5})}
			metadata := input.Report.CustomSamples["queue"]
			metadata.SampleID = "e6d7a562-05aa-440b-a9e0-3bddfba76dcf"
			input.Report.CustomSamples["queue"] = metadata
			if !acceptWALIngest(t, metrics, input) {
				t.Fatal("lost durable receipt after abrupt restart")
			}
			assertRestoredAgentWALBatch(t, metrics, input)
			latest, err := metrics.LatestAgentReceipt(t.Context(), "node")
			if err != nil || latest == nil {
				t.Fatal("restored receipt missing", err)
			}
			if stage == "sync" {
				if latest.SampleID != "d6d7a562-05aa-440b-a9e0-3bddfba76dcf" {
					t.Fatal("complete unacknowledged WAL frame not recovered")
				}
			} else if latest.SampleID != input.Report.SampleID {
				t.Fatal("partial frame became a receipt")
			}
		})
	}
}

func walIngestionPolicy(context.Context) (MetricReportSample, error) {
	return MetricReportSample{Custom: []model.MetricValue{{Name: "custom_queue", Value: 7}}}, nil
}

func acceptWALIngest(t *testing.T, metrics *MTSStore, input AgentReportInput) bool {
	t.Helper()
	attempt, err := metrics.PrepareAgentReport(t.Context(), input, walIngestionPolicy)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	duplicate, err := attempt.Commit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return duplicate
}

func assertRestoredAgentWALBatch(t *testing.T, metrics *MTSStore, input AgentReportInput) {
	t.Helper()
	if healthy, reasons := metrics.Health(); !healthy {
		t.Fatal("replayed engine is not ready", reasons)
	}
	names := append(model.MetricNames(), trafficReceivedDelta, trafficSentDelta, "custom_queue")
	values, err := metrics.QueryMetrics(t.Context(), names, input.Report.CapturedAt.Add(-time.Nanosecond), input.Report.CapturedAt.Add(time.Nanosecond), input.Identity.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if len(values[name]) != 1 || !values[name][0].Timestamp.Equal(input.Report.CapturedAt) {
			t.Fatal("acknowledged frame lost numeric field or nanosecond", name)
		}
	}
	if values["custom_queue"][0].Value != 7 || values["cpu"][0].Value != 25 {
		t.Fatal("replayed batch changed metrics")
	}
}
