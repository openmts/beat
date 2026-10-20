package store

import (
	"context"
	"sync"
	"sync/atomic"
)

// MTSSnapshot 在元数据快照与 MTS 导出期间冻结所有写入和已准入 Agent contact。
type MTSSnapshot struct {
	store  *MTSStore
	unlock func()
	once   sync.Once
	closed atomic.Bool
}

func (s *MTSStore) BeginMetricSnapshot(ctx context.Context) (*MTSSnapshot, error) {
	unlock, err := s.lockMetricOperation(ctx, true)
	if err != nil {
		return nil, err
	}
	if err := s.ensureOpen(); err != nil {
		unlock()
		return nil, err
	}
	return &MTSSnapshot{store: s, unlock: unlock}, nil
}

func (snapshot *MTSSnapshot) ExportCatalogRows(ctx context.Context, request MetricExport, consume MTSRowConsumer) (int64, error) {
	if snapshot.closed.Load() {
		return 0, ErrMTSStoreClosed
	}
	return snapshot.store.exportCatalogRowsLocked(ctx, request, consume)
}

func (snapshot *MTSSnapshot) Close() {
	snapshot.once.Do(func() { snapshot.closed.Store(true); snapshot.unlock() })
}
