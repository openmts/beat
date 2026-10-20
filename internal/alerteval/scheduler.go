package alerteval

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultWorkers = 4

const MaximumWorkers = 8

// DuePair 的排序键固定在一轮开始时，避免失败项挤占后续节点。
type DuePair struct {
	RuleID, NodeID, DeviceID string
	DueAt                    time.Time
}

type RoundSource interface {
	Due(context.Context, DueQuery) ([]DuePair, error)
	Evaluate(context.Context, DuePair) error
}

type DueQuery struct {
	At    time.Time
	After *DuePair
	Limit int
}

type RoundResult struct {
	Completed, Failed, Admitted int64
	Deferred                    int64
	BacklogComplete             bool
	OldestDueAge                time.Duration
	Degraded                    bool
}

type Scheduler struct {
	source  RoundSource
	workers int
	running atomic.Bool
}

func NewScheduler(source RoundSource, workers int) (*Scheduler, error) {
	if workers == 0 {
		workers = DefaultWorkers
	}
	if source == nil || workers < 1 || workers > MaximumWorkers {
		return nil, ErrInvalidPolicy
	}
	return &Scheduler{source: source, workers: workers}, nil
}

// Round 不重叠且最多运行25秒；256仅限制单次选择，不限制整轮总吞吐。
func (s *Scheduler) Round(ctx context.Context, at time.Time) (RoundResult, error) {
	if !s.running.CompareAndSwap(false, true) {
		return RoundResult{}, ErrConflict
	}
	defer s.running.Store(false)
	ctx, cancel := context.WithTimeout(ctx, RoundBudget)
	defer cancel()
	var result RoundResult
	query := DueQuery{At: at, Limit: BatchSize}
	for ctx.Err() == nil {
		pairs, err := s.source.Due(ctx, query)
		if err != nil {
			return result, err
		}
		if len(pairs) == 0 {
			return result, nil
		}
		if err := validateDueBatch(pairs, query); err != nil {
			return result, err
		}
		if result.Admitted == 0 {
			result.OldestDueAge = at.Sub(pairs[0].DueAt)
			result.Degraded = result.OldestDueAge > MaximumPollGap
		}
		s.evaluateBatch(ctx, pairs, &result)
		last := pairs[len(pairs)-1]
		query.After = &last
	}
	return result, ctx.Err()
}

func validateDueBatch(pairs []DuePair, query DueQuery) error {
	if len(pairs) > query.Limit {
		return ErrCapacity
	}
	previous := query.After
	for index := range pairs {
		pair := &pairs[index]
		if pair.RuleID == "" || pair.NodeID == "" || pair.DueAt.After(query.At) {
			return ErrInvalidObservation
		}
		if previous != nil && !duePairBefore(*previous, *pair) {
			return ErrInvalidObservation
		}
		previous = pair
	}
	return nil
}

func duePairBefore(a, b DuePair) bool {
	if !a.DueAt.Equal(b.DueAt) {
		return a.DueAt.Before(b.DueAt)
	}
	if a.RuleID != b.RuleID {
		return a.RuleID < b.RuleID
	}
	if a.NodeID != b.NodeID {
		return a.NodeID < b.NodeID
	}
	return a.DeviceID < b.DeviceID
}

func (s *Scheduler) evaluateBatch(ctx context.Context, pairs []DuePair, result *RoundResult) {
	jobs := make(chan DuePair)
	var workers sync.WaitGroup
	var completed, failed atomic.Int64
	for range s.workers {
		workers.Go(func() {
			for pair := range jobs {
				err := s.source.Evaluate(ctx, pair)
				if err == nil {
					completed.Add(1)
				} else if !errors.Is(err, ErrConflict) {
					failed.Add(1)
				}
			}
		})
	}
	for _, pair := range pairs {
		select {
		case jobs <- pair:
			result.Admitted++
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			result.Completed += completed.Load()
			result.Failed += failed.Load()
			return
		}
	}
	close(jobs)
	workers.Wait()
	result.Completed += completed.Load()
	result.Failed += failed.Load()
}
