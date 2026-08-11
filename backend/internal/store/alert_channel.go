package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const alertChannelColumns = "id, name, channel_type, config, enabled, created_at, updated_at"

type AlertChannelStore struct {
	db *sql.DB
}

func NewAlertChannelStore(db *sql.DB) *AlertChannelStore {
	return &AlertChannelStore{db: db}
}

func (s *AlertChannelStore) GetAlertChannel(ctx context.Context, id string) (*model.AlertChannel, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+alertChannelColumns+" FROM alert_channels WHERE id = ?",
		id,
	)
	channel, err := scanAlertChannel(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying alert channel: %w", err)
	}
	return &channel, nil
}

func (s *AlertChannelStore) ListAlertChannels(ctx context.Context) ([]model.AlertChannel, error) {
	return s.listChannels(ctx, "SELECT "+alertChannelColumns+" FROM alert_channels ORDER BY created_at ASC")
}

func (s *AlertChannelStore) CreateAlertChannel(
	ctx context.Context,
	channel *model.AlertChannel,
) (*model.AlertChannel, error) {
	now := model.NowUTC()
	channel.ID = uuid.New().String()
	channel.CreatedAt = now
	channel.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO alert_channels ("+alertChannelColumns+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		channel.ID,
		channel.Name,
		channel.ChannelType,
		channel.Config,
		channel.Enabled,
		channel.CreatedAt,
		channel.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting alert channel: %w", err)
	}
	return channel, nil
}

func (s *AlertChannelStore) UpdateAlertChannel(
	ctx context.Context,
	id string,
	channel *model.AlertChannel,
) (*model.AlertChannel, error) {
	channel.UpdatedAt = model.NowUTC()
	_, err := s.db.ExecContext(ctx,
		"UPDATE alert_channels SET name = ?, channel_type = ?, config = ?, enabled = ?, updated_at = ? WHERE id = ?",
		channel.Name,
		channel.ChannelType,
		channel.Config,
		channel.Enabled,
		channel.UpdatedAt,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating alert channel: %w", err)
	}
	channel.ID = id
	return channel, nil
}

func (s *AlertChannelStore) DeleteAlertChannel(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM alert_channels WHERE id = ?", id); err != nil {
		return fmt.Errorf("deleting alert channel: %w", err)
	}
	return nil
}

func (s *AlertChannelStore) ListEnabledChannels(ctx context.Context) ([]model.AlertChannel, error) {
	query := "SELECT " + alertChannelColumns +
		" FROM alert_channels WHERE enabled = 1 ORDER BY created_at ASC"
	return s.listChannels(ctx, query)
}

func (s *AlertChannelStore) listChannels(ctx context.Context, query string) ([]model.AlertChannel, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying alert channels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	channels := []model.AlertChannel{}
	for rows.Next() {
		channel, scanErr := scanAlertChannel(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning alert channel: %w", scanErr)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating alert channels: %w", err)
	}
	return channels, nil
}

type alertChannelScanner func(...any) error

func scanAlertChannel(scan alertChannelScanner) (model.AlertChannel, error) {
	var channel model.AlertChannel
	err := scan(
		&channel.ID,
		&channel.Name,
		&channel.ChannelType,
		&channel.Config,
		&channel.Enabled,
		&channel.CreatedAt,
		&channel.UpdatedAt,
	)
	return channel, err
}
