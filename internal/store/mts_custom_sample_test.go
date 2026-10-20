package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

type customSampleFixture struct {
	metrics *MTSStore
	nodeID  string
	at      time.Time
}

// newCustomSampleFixture 通过真实上报路径写入一个带来源元数据的自定义指标样本，
// 自定义指标必须先注册并绑定到节点，否则上报按契约被拒绝。
func newCustomSampleFixture(t *testing.T) customSampleFixture {
	t.Helper()
	sqlite := setupTestDB(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "custom-sample-node", "192.0.2.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	customs := NewCustomMetricStore(sqlite.DB)
	metric, err := customs.Create(t.Context(), model.CustomMetric{Name: "queue", ShellCommand: "printf 7",
		IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatalf("create custom metric: %v", err)
	}
	if err := customs.SetMetricNodes(t.Context(), metric.ID, []string{node.ID}); err != nil {
		t.Fatalf("bind custom metric: %v", err)
	}
	at := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	sources := map[string]model.CustomSampleMetadata{"queue": testutil.CustomSourceFixture(at, *metric)}
	catalog := NewMetricCatalogStore(sqlite.DB)
	policy := AgentMetricPolicy(func(ctx context.Context) (MetricReportSample, error) {
		return catalog.PrepareAgentPolicy(ctx, AgentMetricPolicyInput{NodeID: node.ID,
			Values: map[string]float64{"queue": 7}, Samples: sources})
	})
	metrics := metricQueryMTS(t)
	input := ingestInput(at, 100)
	input.Identity.NodeID = node.ID
	input.Report.CustomMetrics = map[string]float64{"queue": 7}
	input.Report.CustomSamples = sources
	attempt, err := metrics.PrepareAgentReport(t.Context(), input, policy)
	if err != nil {
		t.Fatalf("prepare custom sample report: %v", err)
	}
	defer attempt.Close()
	duplicate, err := attempt.Commit(t.Context())
	if err != nil || duplicate {
		t.Fatalf("commit custom sample report: duplicate = %v, err = %v", duplicate, err)
	}
	return customSampleFixture{metrics: metrics, nodeID: node.ID, at: at}
}

func TestQueryCustomSampleReturnsSourceProvenance(t *testing.T) {
	fixture := newCustomSampleFixture(t)
	evidence, err := fixture.metrics.QueryCustomSample(t.Context(), fixture.nodeID,
		model.CustomMetricPrefix+"queue")
	if err != nil || evidence == nil {
		t.Fatalf("custom evidence = %+v, err = %v", evidence, err)
	}
	if evidence.Value != 7 || !evidence.ReportAt.Equal(fixture.at) {
		t.Fatalf("custom evidence = %+v", evidence)
	}
	if evidence.Source.SampleID == "" || !evidence.Source.CapturedAt.Equal(fixture.at) {
		t.Fatalf("custom source = %+v", evidence.Source)
	}

	// 非自定义前缀属于契约违规，必须直接拒绝而不是回落到空样本。
	if _, err := fixture.metrics.QueryCustomSample(t.Context(), fixture.nodeID, "cpu"); !errors.Is(err, ErrMetricIngestion) {
		t.Fatalf("prefixed metric error = %v", err)
	}

	// 有前缀但没有样本的指标返回空证据，调用方按“无数据”渲染。
	absent, err := fixture.metrics.QueryCustomSample(t.Context(), fixture.nodeID, model.CustomMetricPrefix+"absent")
	if err != nil || absent != nil {
		t.Fatalf("absent custom evidence = %+v, err = %v", absent, err)
	}

	// 同一时间点缺少来源条目时无法证明数据出处，必须报错。
	if err := fixture.metrics.WriteMetric(t.Context(), fixture.nodeID,
		model.CustomMetricPrefix+"other", 3, fixture.at); err != nil {
		t.Fatalf("write unrelated custom metric: %v", err)
	}
	if _, err := fixture.metrics.QueryCustomSample(t.Context(), fixture.nodeID,
		model.CustomMetricPrefix+"other"); !errors.Is(err, ErrCustomSampleSourceUnavailable) {
		t.Fatalf("missing provenance error = %v", err)
	}
}

func TestQueryCustomSampleRejectsCorruptReceiptMetadata(t *testing.T) {
	metrics := metricQueryMTS(t)
	at := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	row := receiptTestRow(t)
	row.Timestamp = at.UnixNano()
	row.Fields["custom_samples"] = mts.StringValue("broken")
	if err := metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: model.AgentReceiptMeasurement,
		Tags: row.Tags, Fields: row.Fields, Timestamp: row.Timestamp}}); err != nil {
		t.Fatalf("import receipt: %v", err)
	}
	if err := metrics.WriteMetric(t.Context(), "node", model.CustomMetricPrefix+"queue", 5, at); err != nil {
		t.Fatalf("write custom point: %v", err)
	}
	if _, err := metrics.QueryCustomSample(t.Context(), "node", model.CustomMetricPrefix+"queue"); !errors.Is(err, ErrAgentReceiptInvalid) {
		t.Fatalf("corrupt receipt error = %v", err)
	}
}

func TestQueryCustomSampleRejectsClosedStore(t *testing.T) {
	metrics := setupClosedTestMTS(t)
	_, err := metrics.QueryCustomSample(t.Context(), "node-closed", model.CustomMetricPrefix+"queue")
	assertClosedError(t, "QueryCustomSample", err)
}
