package server

import (
	"context"
	"fmt"
	"time"

	"beat/internal/taskmetrics"
)

func (s *Server) persistShellTaskMetrics(ctx context.Context, runID string) error {
	run, err := s.store.GetTaskRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("read completed task run: %w", err)
	}
	if run.Status != "success" {
		return nil
	}
	task, err := s.store.GetTaskDefinition(ctx, run.TaskID)
	if err != nil {
		return fmt.Errorf("read task definition: %w", err)
	}
	if task.Kind != taskKindShell {
		return nil
	}
	cfg, err := taskmetrics.ParseConfig(task.Config)
	if err != nil {
		return fmt.Errorf("parse task metric config: %w", err)
	}
	if !cfg.MetricSchema.Enabled {
		return nil
	}
	values, err := taskmetrics.Extract(cfg, run.Output)
	if err != nil {
		return fmt.Errorf("extract task metrics: %w", err)
	}
	if len(values) == 0 {
		return nil
	}
	timestamp := time.Now().UTC()
	if run.CompletedAt != nil {
		timestamp = run.CompletedAt.UTC()
	}
	return s.tsdb.AppendTaskMetrics(ctx, run.NodeID, cfg.MetricSchema.Measurement, run.TaskID, run.TaskName, timestamp, values)
}
