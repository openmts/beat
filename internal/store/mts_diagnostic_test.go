package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
)

func diagnosticMTS(t *testing.T) *MTSStore {
	t.Helper()
	mts, err := NewMTSStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mts.Close() })
	return mts
}

func mtrSample(hop int, value float64) model.DiagnosticSample {
	return model.DiagnosticSample{JobID: "job-1", NodeID: "node-1", Metric: model.DiagnosticMetricMTRLatencyMS,
		Hop: hop, Stat: model.DiagnosticStatAvg, Value: value}
}

func TestMTSDiagnosticWriteAndRead(t *testing.T) {
	mts := diagnosticMTS(t)
	from := time.Unix(1_700_000_000, 0).UTC()
	batch := DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-1", At: from,
		Samples: []model.DiagnosticSample{mtrSample(1, 10.5), mtrSample(2, 20.5)}}
	if err := mts.WriteDiagnosticSamples(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	query := DiagnosticQuery{NodeID: "node-1", JobID: "job-1", Metric: model.DiagnosticMetricMTRLatencyMS,
		From: from.Add(-time.Minute), To: from.Add(time.Minute), Hop: 1, Stat: model.DiagnosticStatAvg, Limit: 100}
	points, err := mts.ReadDiagnosticPoints(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Value != 10.5 || !points[0].Timestamp.Equal(from) {
		t.Fatalf("points = %+v", points)
	}
	// 标签过滤必须真的生效：换一个 hop 读到的是另一条序列。
	query.Hop = 2
	if points, err = mts.ReadDiagnosticPoints(t.Context(), query); err != nil || len(points) != 1 || points[0].Value != 20.5 {
		t.Fatalf("hop filter = %+v / %v", points, err)
	}
	// iperf3 序列按 direction/protocol/role 过滤。
	iperf := model.DiagnosticSample{JobID: "job-2", NodeID: "node-1", Metric: model.DiagnosticMetricIperfBytesPerSecond,
		Direction: model.DiagnosticDirectionForward, Protocol: model.DiagnosticProtocolTCP,
		Role: model.DiagnosticRoleSender, Value: 1234}
	if err := mts.WriteDiagnosticSamples(t.Context(), DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-2",
		At: from.Add(time.Second), Samples: []model.DiagnosticSample{iperf}}); err != nil {
		t.Fatal(err)
	}
	iperfQuery := DiagnosticQuery{NodeID: "node-1", JobID: "job-2", Metric: model.DiagnosticMetricIperfBytesPerSecond,
		From: from, To: from.Add(time.Minute), Direction: model.DiagnosticDirectionForward,
		Protocol: model.DiagnosticProtocolTCP, Role: model.DiagnosticRoleSender, Limit: 10}
	if points, err = mts.ReadDiagnosticPoints(t.Context(), iperfQuery); err != nil || len(points) != 1 || points[0].Value != 1234 {
		t.Fatalf("iperf points = %+v / %v", points, err)
	}
	// 作业删除后序列清空，其它作业不受影响。
	if err := mts.DeleteDiagnosticJob(t.Context(), "job-1"); err != nil {
		t.Fatal(err)
	}
	if points, err = mts.ReadDiagnosticPoints(t.Context(), query); err != nil || len(points) != 0 {
		t.Fatalf("after delete = %+v / %v", points, err)
	}
	if points, err = mts.ReadDiagnosticPoints(t.Context(), iperfQuery); err != nil || len(points) != 1 {
		t.Fatalf("other job deleted = %+v / %v", points, err)
	}
}

func TestMTSDiagnosticWriteRejectsInvalidBatches(t *testing.T) {
	mts := diagnosticMTS(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	cases := []struct {
		name  string
		batch DiagnosticSampleBatch
	}{
		{"zero timestamp", DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-1", Samples: []model.DiagnosticSample{mtrSample(1, 1)}}},
		{"empty samples", DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-1", At: at}},
		{"mismatched node", DiagnosticSampleBatch{NodeID: "node-2", JobID: "job-1", At: at, Samples: []model.DiagnosticSample{mtrSample(1, 1)}}},
		{"mismatched job", DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-2", At: at, Samples: []model.DiagnosticSample{mtrSample(1, 1)}}},
		{"invalid sample", DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-1", At: at,
			Samples: []model.DiagnosticSample{{JobID: "job-1", NodeID: "node-1", Metric: "cpu", Value: 1}}}},
		{"batch above limit", DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-1", At: at,
			Samples: oversizedDiagnosticBatch()}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := mts.WriteDiagnosticSamples(t.Context(), testCase.batch); !errors.Is(err, ErrDiagnosticSampleRejected) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func oversizedDiagnosticBatch() []model.DiagnosticSample {
	samples := make([]model.DiagnosticSample, model.MaximumDiagnosticSampleBatch+1)
	for index := range samples {
		samples[index] = mtrSample(1, float64(index))
	}
	return samples
}

func TestMTSDiagnosticQueryValidation(t *testing.T) {
	mts := diagnosticMTS(t)
	from := time.Unix(1_700_000_000, 0).UTC()
	base := DiagnosticQuery{NodeID: "node-1", JobID: "job-1", Metric: model.DiagnosticMetricMTRLatencyMS,
		From: from, To: from.Add(time.Minute), Hop: 1, Stat: model.DiagnosticStatAvg, Limit: 100}
	cases := []struct {
		name   string
		mutate func(*DiagnosticQuery)
	}{
		{"missing node", func(query *DiagnosticQuery) { query.NodeID = "" }},
		{"missing job", func(query *DiagnosticQuery) { query.JobID = "" }},
		{"unregistered metric", func(query *DiagnosticQuery) { query.Metric = "cpu" }},
		{"node too long", func(query *DiagnosticQuery) {
			query.NodeID = strings.Repeat("n", model.MaximumDiagnosticReferenceBytes+1)
		}},
		{"job too long", func(query *DiagnosticQuery) {
			query.JobID = strings.Repeat("j", model.MaximumDiagnosticReferenceBytes+1)
		}},
		{"limit too small", func(query *DiagnosticQuery) { query.Limit = metricquery.MinimumPoints - 1 }},
		{"limit too large", func(query *DiagnosticQuery) { query.Limit = 20001 }},
		{"inverted range", func(query *DiagnosticQuery) { query.To = query.From }},
		{"range too long", func(query *DiagnosticQuery) { query.To = query.From.Add(metricquery.MaximumRange + time.Hour) }},
		{"negative start", func(query *DiagnosticQuery) { query.From = time.Unix(-60, 0).UTC() }},
		{"invalid mtr tags", func(query *DiagnosticQuery) { query.Hop = 0 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			query := base
			testCase.mutate(&query)
			if _, err := mts.ReadDiagnosticPoints(t.Context(), query); !errors.Is(err, metricquery.ErrInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if err := mts.DeleteDiagnosticJob(t.Context(), ""); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("empty job delete = %v", err)
	}
	if err := mts.DeleteDiagnosticJob(t.Context(), strings.Repeat("j", model.MaximumDiagnosticReferenceBytes+1)); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("long job delete = %v", err)
	}
	// 关闭存储后读写都报错，而不是静默成功。
	closed := diagnosticMTS(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closed.WriteDiagnosticSamples(t.Context(), DiagnosticSampleBatch{NodeID: "node-1", JobID: "job-1",
		At: from, Samples: []model.DiagnosticSample{mtrSample(1, 1)}}); err == nil {
		t.Fatal("closed store accepted samples")
	}
	if _, err := closed.ReadDiagnosticPoints(t.Context(), base); err == nil {
		t.Fatal("closed store served a query")
	}
	if err := closed.DeleteDiagnosticJob(t.Context(), "job-1"); err == nil {
		t.Fatal("closed store deleted a job")
	}
}
