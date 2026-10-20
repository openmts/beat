package store

import (
	"context"
	"time"
)

const metricOperationPoll = 5 * time.Millisecond

func (s *MTSStore) lockMetricOperation(ctx context.Context, exclusive bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if unlock, ok := s.tryMetricOperation(exclusive); ok {
		return unlock, nil
	}
	ticker := time.NewTicker(metricOperationPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if unlock, ok := s.tryMetricOperation(exclusive); ok {
				return unlock, nil
			}
		}
	}
}

func (s *MTSStore) tryMetricOperation(exclusive bool) (func(), bool) {
	if exclusive {
		if s.operationMu.TryLock() {
			return s.operationMu.Unlock, true
		}
		return nil, false
	}
	if s.operationMu.TryRLock() {
		return s.operationMu.RUnlock, true
	}
	return nil, false
}
