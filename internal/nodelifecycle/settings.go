package nodelifecycle

import (
	"context"
	"encoding/json"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

type rowScanner interface{ Scan(...any) error }

const settingsQuery = `SELECT timezone,lead_days,post_days,public_country,public_expiry,public_price FROM lifecycle_settings WHERE id=1`

func scanSettings(row rowScanner) (model.LifecycleSettings, error) {
	var value model.LifecycleSettings
	var lead, post string
	if err := row.Scan(&value.Timezone, &lead, &post, &value.PublicCountry, &value.PublicExpiry, &value.PublicPrice); err != nil {
		return value, ErrUnavailable
	}
	if err := json.Unmarshal([]byte(lead), &value.LeadDays); err != nil {
		return value, ErrUnavailable
	}
	if err := json.Unmarshal([]byte(post), &value.PostDays); err != nil {
		return value, ErrUnavailable
	}
	if value.LeadDays == nil {
		value.LeadDays = []int{}
	}
	if value.PostDays == nil {
		value.PostDays = []int{}
	}
	return value, ValidateSettings(value)
}

func (s *Service) Settings(ctx context.Context, actor Actor) (model.LifecycleSettings, error) {
	var value model.LifecycleSettings
	err := s.write(ctx, func(tx *transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(actor, false, false); err != nil {
			return nil, err
		}
		var err error
		value, err = scanSettings(tx.db.QueryRowContext(ctx, settingsQuery))
		return nil, err
	})
	return value, err
}

func (s *Service) SaveSettings(ctx context.Context, actor Actor, input model.LifecycleSettings) (model.LifecycleSettings, error) {
	if err := ValidateSettings(input); err != nil {
		return model.LifecycleSettings{}, err
	}
	lead, err := json.Marshal(input.LeadDays)
	if err != nil {
		return model.LifecycleSettings{}, ErrInvalid
	}
	post, err := json.Marshal(input.PostDays)
	if err != nil {
		return model.LifecycleSettings{}, ErrInvalid
	}
	err = s.write(ctx, func(tx *transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(actor, true, true); err != nil {
			return nil, err
		}
		_, err := tx.db.ExecContext(ctx, `UPDATE lifecycle_settings SET timezone=?,lead_days=?,post_days=?,public_country=?,public_expiry=?,public_price=?,updated_at=? WHERE id=1`, input.Timezone, string(lead), string(post), input.PublicCountry, input.PublicExpiry, input.PublicPrice, tx.now)
		if err != nil {
			return nil, ErrUnavailable
		}
		return nil, tx.audit(actor, "lifecycle.settings.update", "global")
	})
	if err != nil {
		return model.LifecycleSettings{}, err
	}
	return s.Settings(ctx, actor)
}
