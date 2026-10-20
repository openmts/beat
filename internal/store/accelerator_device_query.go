package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

func (s *NodeStore) MetricDevice(ctx context.Context, selection model.AcceleratorDeviceSelection) (*model.AcceleratorDevicePublic, error) {
	if !model.ValidAcceleratorDeviceID(selection.DeviceID) {
		return nil, ErrMetricIngestion
	}
	var value model.AcceleratorDevicePublic
	query := `SELECT a.device_id,a.ordinal,a.vendor,a.model FROM node_accelerators a JOIN nodes n ON n.id=a.node_id WHERE a.node_id=? AND a.device_id=?`
	if selection.PublicOnly {
		query += ` AND n.is_public=1 AND a.inactive_at IS NULL`
	}
	err := s.db.QueryRowContext(ctx, query, selection.NodeID, selection.DeviceID).Scan(&value.DeviceID, &value.Ordinal, &value.Vendor, &value.Model)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &value, err
}
