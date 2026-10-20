package accelerators

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type Config struct {
	Inventory *store.AcceleratorStore
	Metrics   *store.MTSStore
	Catalog   *store.MetricCatalogStore
	Now       func() time.Time
}

type Service struct {
	inventory *store.AcceleratorStore
	metrics   *store.MTSStore
	catalog   *store.MetricCatalogStore
	now       func() time.Time
	observer  func(error)
	failed    atomic.Bool
}

func NewService(config Config) (*Service, error) {
	if config.Inventory == nil || config.Metrics == nil || config.Catalog == nil {
		return nil, store.ErrAcceleratorUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{inventory: config.Inventory, metrics: config.Metrics, catalog: config.Catalog, now: config.Now}, nil
}

func (s *Service) SetObserver(observer func(error)) { s.observer = observer }

func (s *Service) report(err error) {
	s.failed.Store(err != nil)
	if s.observer != nil {
		s.observer(err)
	}
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		err := s.Reconcile(ctx)
		s.report(err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) Reconcile(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := s.inventory.PendingNodes(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, nodeID := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err = s.reconcileNode(ctx, nodeID, ""); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Service) PublishReport(ctx context.Context, nodeID, reportID string) error {
	err := s.reconcileNode(ctx, nodeID, reportID)
	s.report(err)
	return err
}

func (s *Service) reconcileNode(ctx context.Context, nodeID, reportID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pending, err := s.inventory.Pending(ctx, nodeID)
	if err != nil || pending == nil {
		return err
	}
	if reportID != "" && pending.ReportID != reportID {
		return nil
	}
	receipt, err := s.metrics.AcceleratorSampleAt(ctx, nodeID, pending.Snapshot.CapturedAt.UnixNano())
	if err != nil {
		return err
	}
	if receipt == nil {
		return s.missingSample(ctx, *pending, reportID != "")
	}
	err = s.inventory.Publish(ctx, store.AcceleratorPublication{Pending: *pending, Receipt: *receipt})
	if errors.Is(err, store.ErrAcceleratorPolicy) || errors.Is(err, store.ErrAgentCredentialRejected) || errors.Is(err, store.ErrAgentSampleOrder) {
		return s.inventory.DiscardPending(ctx, *pending)
	}
	return err
}

func (s *Service) missingSample(ctx context.Context, pending store.AcceleratorPending, required bool) error {
	if s.now().Sub(pending.CreatedAt) > model.MaximumAgentSampleAge {
		return s.inventory.DiscardPending(ctx, pending)
	}
	if required {
		return store.ErrAcceleratorUnavailable
	}
	return nil
}

func (s *Service) Ready(ctx context.Context) error {
	enabled, err := s.inventory.Enabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if err := s.inventory.Ready(ctx); err != nil {
		return err
	}
	healthy, _ := s.metrics.Health()
	if !healthy || s.failed.Load() {
		return store.ErrAcceleratorUnavailable
	}
	return nil
}

func (s *Service) AgentPolicy(ctx context.Context, nodeID string, installation bool) (model.AcceleratorPolicy, error) {
	return s.inventory.AgentPolicy(ctx, nodeID, installation)
}

func (s *Service) Policy(ctx context.Context, actor store.AcceleratorActor, scope store.AcceleratorPolicyScope) (model.AcceleratorPolicy, error) {
	return s.inventory.Policy(ctx, actor, scope)
}

func (s *Service) SavePolicy(ctx context.Context, actor store.AcceleratorActor, change store.AcceleratorPolicyChange) (model.AcceleratorPolicy, error) {
	return s.inventory.SavePolicy(ctx, actor, change)
}

func (s *Service) Stats(ctx context.Context) (store.AcceleratorStats, error) {
	return s.inventory.Stats(ctx)
}
