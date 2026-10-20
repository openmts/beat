package store

import (
	"context"

	"github.com/beat/backend/internal/model"
)

func (store *DashboardStore) Delete(ctx context.Context, request DashboardDelete) error {
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	current, err := readDashboard(ctx, transaction, request.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrDashboardNotFound
	}
	if current.value.Scope != "personal" {
		return ErrDashboardForbidden
	}
	mutation := dashboardMutation{ctx: ctx, transaction: transaction,
		write: DashboardWrite{Value: current.value, ExpectedRevision: request.ExpectedRevision, Actor: request.Actor}}
	if err := mutation.prepare(); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, "DELETE FROM metric_dashboards WHERE id=? AND revision=?", request.ID, request.ExpectedRevision)
	if err := requireDashboardWrite(result, err); err != nil {
		return err
	}
	if err := mutation.audit("metrics.dashboard.delete"); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *DashboardStore) Rollback(ctx context.Context, request DashboardDelete) (*model.MetricDashboard, error) {
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	current, err := readDashboard(ctx, transaction, request.ID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.previous == "" {
		return nil, ErrDashboardNotFound
	}
	mutation := dashboardMutation{ctx: ctx, transaction: transaction,
		write: DashboardWrite{Value: current.value, ExpectedRevision: request.ExpectedRevision, Actor: request.Actor}}
	if err := mutation.prepare(); err != nil {
		return nil, err
	}
	previous, err := decodePreviousDashboard(current.previous)
	if err != nil {
		return nil, err
	}
	mutation.write.Value.Definition, mutation.encoded = previous, current.previous
	if err := mutation.persist(); err != nil {
		return nil, err
	}
	if err := mutation.audit("metrics.dashboard.rollback"); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return &mutation.write.Value, nil
}
