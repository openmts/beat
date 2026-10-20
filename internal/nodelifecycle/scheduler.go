package nodelifecycle

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/notificationqueue"
)

const schedulerPageSize = 128

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Tick 游标分页调度；每个事件或推进在自己的immediate事务内完成。
func (s *Service) Tick(ctx context.Context) (resultErr error) {
	defer func() { s.observe(resultErr) }()
	cursor := ""
	for {
		ids, err := s.page(ctx, cursor)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err = s.remind(ctx, id); err != nil {
				resultErr = firstError(resultErr, err)
				continue
			}
			if err = s.advance(ctx, id); err != nil {
				resultErr = firstError(resultErr, err)
			}
		}
		if len(ids) < schedulerPageSize {
			break
		}
		cursor = ids[len(ids)-1]
	}
	if resultErr != nil {
		return resultErr
	}
	if err := s.Prune(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE lifecycle_settings SET last_success_at=? WHERE id=1`, s.now())
	return err
}

func (s *Service) page(ctx context.Context, cursor string) (ids []string, resultErr error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM node_lifecycle WHERE id>? AND expires_on!='' ORDER BY id LIMIT ?`, cursor, schedulerPageSize)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, ErrUnavailable
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) remind(ctx context.Context, id string) error {
	return s.write(ctx, func(tx *transaction) (*notificationqueue.Request, error) {
		node, err := tx.node(id)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		settings, err := scanSettings(tx.db.QueryRowContext(ctx, settingsQuery))
		if err != nil {
			return nil, err
		}
		_, remaining, err := LifecycleStatus(node.ExpiresOn, tx.now, settings.Timezone)
		if err != nil || remaining == nil {
			return nil, err
		}
		kind, stage := "node.expiring", *remaining
		selected := slices.Contains(settings.LeadDays, stage)
		if stage < 0 {
			kind, stage = "node.expired", -stage
			selected = slices.Contains(settings.PostDays, stage)
		}
		if !selected {
			return nil, nil
		}
		request, err := tx.event(lifecycleEvent{node: node, kind: kind, stage: stage, newDate: node.ExpiresOn, actor: "scheduler"})
		if err != nil || request == nil {
			return request, err
		}
		return request, tx.audit(Actor{UserID: "scheduler"}, "lifecycle.reminder", id)
	})
}

func (s *Service) advance(ctx context.Context, id string) error {
	return s.write(ctx, func(tx *transaction) (*notificationqueue.Request, error) {
		node, err := tx.node(id)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if node.ExpiresOn == "" || node.RenewalMode != "calendar_auto_advance" {
			return nil, nil
		}
		settings, err := scanSettings(tx.db.QueryRowContext(ctx, settingsQuery))
		if err != nil {
			return nil, err
		}
		advanced, err := advanceNode(node, tx.now, settings.Timezone)
		if err != nil {
			return nil, err
		}
		if advanced.Intervals == 0 {
			return nil, nil
		}
		oldDate := node.ExpiresOn
		node.ExpiresOn = advanced.ExpiresOn
		node.periodKey = uuid.NewSHA1(uuid.NameSpaceURL, []byte(node.periodKey+":"+node.ExpiresOn)).String()
		if err = tx.persistNode(Actor{UserID: "scheduler"}, node); err != nil {
			return nil, err
		}
		if err = tx.audit(Actor{UserID: "scheduler"}, "lifecycle.date.advance", id); err != nil {
			return nil, err
		}
		return tx.event(lifecycleEvent{node: node, kind: "node.renewed", oldDate: oldDate, newDate: node.ExpiresOn, intervals: advanced.Intervals, actor: "scheduler"})
	})
}

func firstError(current, next error) error {
	if current == nil {
		return next
	}
	return current
}

func advanceNode(node storedLifecycle, now time.Time, timezone string) (Advancement, error) {
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return Advancement{}, ErrInvalid
	}
	period := CalendarPeriod{ExpiresOn: node.ExpiresOn, Unit: node.IntervalUnit, Count: node.IntervalCount, Anchor: CalendarAnchor{Month: node.AnchorMonth, Day: node.AnchorDay, EndOfMonth: node.AnchorEndOfMonth}}
	return AdvanceUntilFuture(period, now.In(zone).Format(time.DateOnly))
}
