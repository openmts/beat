package nodelifecycle

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

func visible(override string, global bool) bool {
	return override == "public" || (override == "inherit" && global)
}

func (s *Service) PublicLifecycle(ctx context.Context, id string) (*model.LifecyclePublic, error) {
	var public bool
	if err := s.db.QueryRowContext(ctx, `SELECT is_public FROM nodes WHERE id=?`, id).Scan(&public); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, ErrUnavailable
	}
	if !public {
		return nil, nil
	}
	value, err := scanLifecycle(s.db.QueryRowContext(ctx, lifecycleQuery, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	settings, err := scanSettings(s.db.QueryRowContext(ctx, settingsQuery))
	if err != nil {
		return nil, err
	}
	return projectLifecycle(value, settings, s.now())
}

func (s *Service) Ready(ctx context.Context) error {
	if _, err := scanSettings(s.db.QueryRowContext(ctx, settingsQuery)); err != nil {
		return err
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_lifecycle`).Scan(&count); err != nil {
		return ErrUnavailable
	}
	return nil
}

func projectLifecycle(value storedLifecycle, settings model.LifecycleSettings, now time.Time) (*model.LifecyclePublic, error) {
	output := &model.LifecyclePublic{}
	var err error
	expiry, price := visible(value.ExpiryVisibility, settings.PublicExpiry), visible(value.PriceVisibility, settings.PublicPrice)
	if !expiry && !price {
		return nil, nil
	}
	if expiry {
		output.ExpiresOn = value.ExpiresOn
		output.Status, output.DaysRemaining, err = LifecycleStatus(value.ExpiresOn, now, settings.Timezone)
		if err != nil {
			return nil, err
		}
	}
	if price {
		output.PriceMicros = value.PriceMicros
		output.Currency = value.Currency
		output.IntervalUnit = value.IntervalUnit
		output.IntervalCount = value.IntervalCount
	}
	return output, nil
}
