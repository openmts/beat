package nodelifecycle

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

type storedLifecycle struct {
	model.NodeLifecycle
	periodKey, name string
}

type NodeChange struct {
	NodeID string
	model.LifecycleWrite
}

const lifecycleQuery = `SELECT id,provider,plan,price_micros,currency,interval_unit,interval_count,expires_on,renewal_mode,
 anchor_month,anchor_day,anchor_end_of_month,private_notes,expiry_visibility,price_visibility,updated_at,period_key FROM node_lifecycle WHERE id=?`

func scanLifecycle(row rowScanner) (storedLifecycle, error) {
	var value storedLifecycle
	err := row.Scan(&value.NodeID, &value.Provider, &value.Plan, &value.PriceMicros, &value.Currency, &value.IntervalUnit, &value.IntervalCount, &value.ExpiresOn, &value.RenewalMode, &value.AnchorMonth, &value.AnchorDay, &value.AnchorEndOfMonth, &value.PrivateNotes, &value.ExpiryVisibility, &value.PriceVisibility, &value.UpdatedAt, &value.periodKey)
	return value, err
}

func (tx *transaction) node(id string) (storedLifecycle, error) {
	var name string
	if err := tx.db.QueryRowContext(tx.ctx, `SELECT name FROM nodes WHERE id=?`, id).Scan(&name); errors.Is(err, sql.ErrNoRows) {
		return storedLifecycle{}, ErrNotFound
	} else if err != nil {
		return storedLifecycle{}, ErrUnavailable
	}
	value, err := scanLifecycle(tx.db.QueryRowContext(tx.ctx, lifecycleQuery, id))
	value.name = name
	if errors.Is(err, sql.ErrNoRows) {
		value.NodeID = id
		value.IntervalUnit = "month"
		value.IntervalCount = 1
		value.RenewalMode = "manual"
		value.ExpiryVisibility = "inherit"
		value.PriceVisibility = "inherit"
		return value, nil
	}
	return value, err
}

func (s *Service) Node(ctx context.Context, actor Actor, id string) (model.NodeLifecycle, error) {
	var value storedLifecycle
	err := s.write(ctx, func(tx *transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(actor, false, false); err != nil {
			return nil, err
		}
		var err error
		value, err = tx.node(id)
		if err != nil {
			return nil, err
		}
		settings, err := scanSettings(tx.db.QueryRowContext(ctx, settingsQuery))
		if err != nil {
			return nil, err
		}
		value.Status, value.DaysRemaining, err = LifecycleStatus(value.ExpiresOn, tx.now, settings.Timezone)
		return nil, err
	})
	return value.NodeLifecycle, err
}

func (s *Service) SaveNode(ctx context.Context, actor Actor, input NodeChange) (model.NodeLifecycle, error) {
	if err := ValidateWrite(input.LifecycleWrite); err != nil {
		return model.NodeLifecycle{}, err
	}
	err := s.write(ctx, func(tx *transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(actor, false, false); err != nil {
			return nil, err
		}
		previous, err := tx.node(input.NodeID)
		if err != nil {
			return nil, err
		}
		next, err := prepareChange(previous, input)
		if err != nil {
			return nil, err
		}
		if err = tx.persistNode(actor, next); err != nil {
			return nil, err
		}
		if err = tx.audit(actor, "lifecycle.node.update", input.NodeID); err != nil {
			return nil, err
		}
		if renewedPeriod(previous, next) {
			return tx.event(lifecycleEvent{node: next, kind: "node.renewed", oldDate: previous.ExpiresOn, newDate: next.ExpiresOn, actor: actor.UserID})
		}
		return nil, nil
	})
	if err != nil {
		return model.NodeLifecycle{}, err
	}
	return s.Node(ctx, actor, input.NodeID)
}

func prepareChange(previous storedLifecycle, input NodeChange) (storedLifecycle, error) {
	if input.RenewalMode == "calendar_auto_advance" && previous.RenewalMode != "calendar_auto_advance" && !input.ConfirmAutoAdvance {
		return storedLifecycle{}, ErrInvalid
	}
	next := previous
	next.LifecycleWrite = input.LifecycleWrite
	next.ConfirmAutoAdvance = false
	if previous.ExpiresOn != input.ExpiresOn {
		next.periodKey = uuid.NewString()
		next.AnchorMonth, next.AnchorDay, next.AnchorEndOfMonth = 0, 0, false
		if input.ExpiresOn != "" {
			anchor, err := AnchorForDate(input.ExpiresOn)
			if err != nil {
				return next, err
			}
			next.AnchorMonth, next.AnchorDay, next.AnchorEndOfMonth = anchor.Month, anchor.Day, anchor.EndOfMonth
		}
	}
	return next, nil
}

func (tx *transaction) persistNode(actor Actor, value storedLifecycle) error {
	_, err := tx.db.ExecContext(tx.ctx, `INSERT INTO node_lifecycle(id,provider,plan,price_micros,currency,interval_unit,interval_count,expires_on,renewal_mode,
 anchor_month,anchor_day,anchor_end_of_month,private_notes,expiry_visibility,price_visibility,created_by,updated_by,created_at,updated_at,period_key)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET provider=excluded.provider,plan=excluded.plan,price_micros=excluded.price_micros,
 currency=excluded.currency,interval_unit=excluded.interval_unit,interval_count=excluded.interval_count,expires_on=excluded.expires_on,renewal_mode=excluded.renewal_mode,
 anchor_month=excluded.anchor_month,anchor_day=excluded.anchor_day,anchor_end_of_month=excluded.anchor_end_of_month,private_notes=excluded.private_notes,
 expiry_visibility=excluded.expiry_visibility,price_visibility=excluded.price_visibility,updated_by=excluded.updated_by,updated_at=excluded.updated_at,period_key=excluded.period_key`,
		value.NodeID, value.Provider, value.Plan, value.PriceMicros, value.Currency, value.IntervalUnit, value.IntervalCount, value.ExpiresOn, value.RenewalMode,
		value.AnchorMonth, value.AnchorDay, value.AnchorEndOfMonth, value.PrivateNotes, value.ExpiryVisibility, value.PriceVisibility, actor.UserID, actor.UserID, tx.now, tx.now, value.periodKey)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func renewedPeriod(previous, next storedLifecycle) bool {
	return previous.ExpiresOn != "" && next.ExpiresOn != "" && previous.ExpiresOn != next.ExpiresOn
}
