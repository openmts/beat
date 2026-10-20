package geoip

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

func Visible(override string, global bool) bool {
	return override == "public" || (override == "inherit" && global)
}

// PublicLocation 不返回原始来源、provider、错误或队列状态。
func (s *Service) PublicLocation(ctx context.Context, id string) (*model.LocationPublic, error) {
	var global, public bool
	var provider string
	err := s.db.QueryRowContext(ctx, `SELECT l.public_country,n.is_public,g.provider FROM lifecycle_settings l JOIN nodes n ON n.id=? CROSS JOIN geoip_settings g WHERE l.id=1 AND g.id='global'`, id).Scan(&global, &public, &provider)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if !public {
		return nil, nil
	}
	value, err := scanLocation(s.db.QueryRowContext(ctx, locationSelect, id), s.now())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if value.Mode == "disabled" || (value.Mode != "manual" && provider == "disabled") || !Visible(value.Visibility, global) {
		return nil, nil
	}
	return &value.LocationPublic, nil
}
