package store

import (
	"context"

	"github.com/beat/backend/internal/model"
)

type AgentMetricPolicyInput struct {
	NodeID  string
	Values  map[string]float64
	Samples map[string]model.CustomSampleMetadata
}

// PrepareAgentPolicy 从同一 SQLite 快照冻结自定义绑定与内置持久化策略。
func (catalog *MetricCatalogStore) PrepareAgentPolicy(ctx context.Context, input AgentMetricPolicyInput) (MetricReportSample, error) {
	nodeID, values := input.NodeID, input.Values
	var sample MetricReportSample
	if err := validateCustomMetricBatch(values); err != nil {
		return sample, err
	}
	tx, err := beginWriteTx(ctx, catalog.db)
	if err != nil {
		return sample, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateConfiguredCustomSources(ctx, tx, input); err != nil {
		return sample, err
	}
	sample.Custom, err = validateCustomBatchFrom(ctx, tx, agentCustomBatch{nodeID: nodeID, values: values})
	if err != nil {
		return sample, err
	}
	sample.EnabledBuiltins, err = builtinPersistenceFrom(ctx, tx)
	if err != nil {
		return sample, err
	}
	if err := tx.Commit(); err != nil {
		return sample, err
	}
	return sample, nil
}
