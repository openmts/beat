package store

import (
	"context"
	"math"
	"time"

	"github.com/beat/backend/internal/model"
)

type AgentMetricSnapshot struct {
	Metrics map[string]float64
	Receipt *model.AgentReceipt
}

func (s *MTSStore) QueryAgentSnapshot(ctx context.Context, nodeID string, allowed map[string]bool) (AgentMetricSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, agentSnapshotTimeout)
	defer cancel()
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return AgentMetricSnapshot{}, err
	}
	defer unlock()
	release, err := s.lockAgentNode(ctx, nodeID)
	if err != nil {
		return AgentMetricSnapshot{}, err
	}
	defer release()
	metrics, err := s.queryLatestFilteredLocked(ctx, nodeID, allowed)
	if err != nil {
		return AgentMetricSnapshot{}, err
	}
	receipt, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: nodeID, end: math.MaxInt64})
	if err != nil {
		return AgentMetricSnapshot{}, err
	}
	return AgentMetricSnapshot{Metrics: metrics, Receipt: receipt}, nil
}

const agentSnapshotTimeout = 5 * time.Second

func (s *MTSStore) lockAgentNode(ctx context.Context, nodeID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lock := s.nodeLock(nodeID)
	if lock.TryLock() {
		return lock.Unlock, nil
	}
	ticker := time.NewTicker(metricOperationPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if lock.TryLock() {
				return lock.Unlock, nil
			}
		}
	}
}
