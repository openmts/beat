package store

import (
	"context"
	"errors"
	"strings"

	"github.com/beat/backend/internal/model"
)

var ErrCustomSourceChanged = errors.New("custom sample was collected under a superseded definition")

func validateConfiguredCustomSources(ctx context.Context, db schemaExecutor, input AgentMetricPolicyInput) error {
	if len(input.Values) != len(input.Samples) {
		return ErrMetricIngestion
	}
	if len(input.Values) == 0 {
		return nil
	}
	arguments := make([]any, 0, len(input.Values)+1)
	arguments = append(arguments, input.NodeID)
	for name := range input.Values {
		if _, ok := input.Samples[name]; !ok {
			return ErrMetricIngestion
		}
		arguments = append(arguments, name)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(input.Values)), ",")
	rows, err := db.QueryContext(ctx, `SELECT m.id,m.name,m.shell_command,m.interval_seconds,m.updated_at,m.enabled AND d.enabled FROM custom_metrics m
		JOIN custom_metric_nodes n ON n.metric_id=m.id JOIN metric_definitions d ON d.metric_key='custom_'||m.name
		WHERE n.node_id=? AND m.name IN (`+placeholders+`)`, arguments...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	found := 0
	for rows.Next() {
		var metric model.CustomMetric
		var enabled bool
		if err := rows.Scan(&metric.ID, &metric.Name, &metric.ShellCommand, &metric.IntervalSeconds, &metric.UpdatedAt, &enabled); err != nil {
			return err
		}
		found++
		if !enabled {
			continue
		}
		source := input.Samples[metric.Name]
		if source.CadenceMS != int64(metric.IntervalSeconds)*1000 || source.DefinitionVersion != model.CustomMetricDefinitionVersion(metric) {
			return ErrCustomSourceChanged
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found != len(input.Values) {
		return ErrMetricIngestion
	}
	return nil
}
