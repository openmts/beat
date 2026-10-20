package diagnostics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/beat/backend/internal/model"
)

const targetColumns = `id, name, purpose, kinds_json, address, port, ip_family, protocol, direction,
	private_address, allowed_addresses_json, resolved_addresses_json, resolved_at, maximum_bitrate_mbps,
	maximum_duration_seconds, maximum_parallel_streams, daily_byte_budget, enabled, expires_at,
	revision, created_by, updated_by, created_at, updated_at`

// ListTargets 返回带显示名称的目标列表；从不返回原始 ID 供选择控件使用。
func (service *Service) ListTargets(ctx context.Context, actor Actor) ([]model.DiagnosticTarget, error) {
	targets := []model.DiagnosticTarget{}
	err := service.read(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		rows, err := tx.tx.QueryContext(tx.ctx, `SELECT `+targetColumns+` FROM diagnostic_targets
			ORDER BY enabled DESC, name, id LIMIT 500`)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			target, err := scanTarget(rows)
			if err != nil {
				return err
			}
			targets = append(targets, target)
		}
		if err := rows.Err(); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return targets, err
}

// GetTarget 返回单个目标；不存在与不可读都按未找到处理。
func (service *Service) GetTarget(ctx context.Context, actor Actor, id string) (model.DiagnosticTarget, error) {
	var target model.DiagnosticTarget
	err := service.read(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		loaded, err := tx.target(id)
		if err != nil {
			return err
		}
		target = loaded
		return nil
	})
	return target, err
}

func (tx transaction) target(id string) (model.DiagnosticTarget, error) {
	if id == "" {
		return model.DiagnosticTarget{}, ErrInvalid
	}
	row := tx.tx.QueryRowContext(tx.ctx, `SELECT `+targetColumns+` FROM diagnostic_targets WHERE id=?`, id)
	target, err := scanTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DiagnosticTarget{}, ErrNotFound
	}
	return target, err
}

type rowScanner interface{ Scan(...any) error }

func scanTarget(row rowScanner) (model.DiagnosticTarget, error) {
	var target model.DiagnosticTarget
	var kinds, allowed, resolved string
	var private bool
	var resolvedAt, expiresAt sql.NullTime
	err := row.Scan(&target.ID, &target.Name, &target.Purpose, &kinds, &target.Address, &target.Port, &target.IPFamily,
		&target.Protocol, &target.Direction, &private, &allowed, &resolved, &resolvedAt, &target.MaximumBitrateMbps,
		&target.MaximumDuration, &target.MaximumParallelStream, &target.DailyByteBudget, &target.Enabled, &expiresAt,
		&target.Revision, &target.CreatedBy, &target.UpdatedBy, &target.CreatedAt, &target.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DiagnosticTarget{}, sql.ErrNoRows
	}
	if err != nil {
		return model.DiagnosticTarget{}, ErrUnavailable
	}
	target.PrivateAddress = private
	if json.Unmarshal([]byte(kinds), &target.Kinds) != nil || json.Unmarshal([]byte(allowed), &target.AllowedAddresses) != nil ||
		json.Unmarshal([]byte(resolved), &target.ResolvedAddresses) != nil {
		return model.DiagnosticTarget{}, ErrUnavailable
	}
	if target.Kinds == nil {
		target.Kinds = []string{}
	}
	if target.AllowedAddresses == nil {
		target.AllowedAddresses = []string{}
	}
	if target.ResolvedAddresses == nil {
		target.ResolvedAddresses = []string{}
	}
	if resolvedAt.Valid {
		at := resolvedAt.Time
		target.ResolvedAt = &at
	}
	if expiresAt.Valid {
		at := expiresAt.Time
		target.ExpiresAt = &at
	}
	return target, nil
}

// usableTarget 在创建作业前复核目标可用性：启用、未过期且策略允许其类别。
func (tx transaction) usableTarget(policy model.DiagnosticPolicy, kind, id string) (model.DiagnosticTarget, error) {
	target, err := tx.target(id)
	if err != nil {
		return model.DiagnosticTarget{}, err
	}
	if !target.Enabled || target.ExpiresAt != nil && !tx.now.Before(*target.ExpiresAt) {
		return model.DiagnosticTarget{}, ErrForbidden
	}
	if !allowsKindIn(target.Kinds, kind) || !allowsKind(policy, kind) {
		return model.DiagnosticTarget{}, ErrForbidden
	}
	if target.PrivateAddress && !policy.AllowPrivateTargets {
		return model.DiagnosticTarget{}, ErrForbidden
	}
	return target, nil
}

func allowsKindIn(kinds []string, kind string) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}
