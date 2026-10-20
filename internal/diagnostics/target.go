package diagnostics

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const resolveTimeout = 5 * time.Second

// TargetChange 是一次目标创建或更新；ID 为空表示创建。
type TargetChange struct {
	ID               string
	ExpectedRevision int
	Input            model.DiagnosticTargetInput
}

// SaveTarget 在 owner 最近重认证后写入目标；地址在审批时解析并逐条放行。
func (service *Service) SaveTarget(ctx context.Context, actor Actor, change TargetChange) (model.DiagnosticTarget, error) {
	target, err := service.prepareTarget(ctx, change)
	if err != nil {
		return model.DiagnosticTarget{}, err
	}
	err = service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		policy, err := tx.policy()
		if err != nil {
			return err
		}
		if err := approveTargetPolicy(policy, target); err != nil {
			return err
		}
		if change.ID == "" {
			target.ID = uuid.NewString()
			target.Revision = 1
			target.CreatedBy, target.UpdatedBy = actor.UserID, actor.UserID
			target.CreatedAt, target.UpdatedAt = tx.now, tx.now
			if err := tx.insertTarget(target); err != nil {
				return err
			}
			return tx.audit(actor.UserID, "diagnostic.target.created", target.ID)
		}
		current, err := tx.target(change.ID)
		if err != nil {
			return err
		}
		if change.ExpectedRevision != current.Revision {
			return ErrConflict
		}
		target.ID, target.Revision = current.ID, current.Revision+1
		target.CreatedBy, target.CreatedAt = current.CreatedBy, current.CreatedAt
		target.UpdatedBy, target.UpdatedAt = actor.UserID, tx.now
		if err := tx.updateTarget(target); err != nil {
			return err
		}
		return tx.audit(actor.UserID, "diagnostic.target.updated", target.ID)
	})
	return target, err
}

// DeleteTarget 拒绝删除仍有活动作业的目标；先删除必须显式取消作业。
func (service *Service) DeleteTarget(ctx context.Context, actor Actor, id string) error {
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		var active int
		if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COUNT(*) FROM diagnostic_jobs WHERE target_id=?
			AND state IN ('queued','leased','running','unknown')`, id).Scan(&active); err != nil {
			return ErrUnavailable
		}
		if active != 0 {
			return ErrConflict
		}
		if err := tx.one(`DELETE FROM diagnostic_targets WHERE id=?`, id); err != nil {
			return err
		}
		if err := tx.exec(`DELETE FROM diagnostic_hop_identities WHERE job_id IN (SELECT id FROM diagnostic_jobs WHERE target_id=?)`, id); err != nil {
			return err
		}
		return tx.audit(actor.UserID, "diagnostic.target.deleted", id)
	})
}

// prepareTarget 在打开写事务前完成解析与地址审批，避免长时间持有写锁。
func (service *Service) prepareTarget(ctx context.Context, change TargetChange) (model.DiagnosticTarget, error) {
	input := change.Input
	target := model.DiagnosticTarget{Name: strings.TrimSpace(input.Name), Purpose: input.Purpose,
		Kinds: append([]string{}, input.Kinds...), Address: input.Address, Port: input.Port, IPFamily: input.IPFamily,
		Protocol: input.Protocol, Direction: input.Direction, PrivateAddress: input.PrivateAddress,
		AllowedAddresses: append([]string{}, input.AllowedAddresses...), MaximumBitrateMbps: input.MaximumBitrateMbps,
		MaximumDuration: input.MaximumDuration, MaximumParallelStream: input.MaximumParallelStream,
		DailyByteBudget: input.DailyByteBudget, Enabled: input.Enabled, ExpiresAt: input.ExpiresAt}
	if target.IPFamily == "" {
		target.IPFamily = model.IPFamilyAuto
	}
	if target.AllowedAddresses == nil {
		target.AllowedAddresses = []string{}
	}
	if err := target.Validate(); err != nil {
		return model.DiagnosticTarget{}, ErrInvalid
	}
	resolved, err := service.resolveTarget(ctx, target)
	if err != nil {
		return model.DiagnosticTarget{}, err
	}
	target.ResolvedAddresses = resolved
	at := service.now()
	target.ResolvedAt = &at
	return target, nil
}

func (service *Service) resolveTarget(ctx context.Context, target model.DiagnosticTarget) ([]string, error) {
	prefixes, err := ParseApprovedPrefixes(target.AllowedAddresses)
	if err != nil {
		return nil, ErrInvalid
	}
	approval := AddressApproval{PrivateAllowed: target.PrivateAddress, Family: target.IPFamily, Approved: prefixes}
	addresses, err := service.lookupAddresses(ctx, target.Address)
	if err != nil || len(addresses) == 0 || len(addresses) > model.MaximumDiagnosticTargetAddresses {
		return nil, ErrInvalid
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if err := ApproveAddress(address, approval); err != nil {
			return nil, ErrForbidden
		}
		result = append(result, normalizeAddress(address).String())
	}
	return result, nil
}

func (service *Service) lookupAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{literal}, nil
	}
	if service.resolve == nil {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	return service.resolve(ctx, host)
}

func approveTargetPolicy(policy model.DiagnosticPolicy, target model.DiagnosticTarget) error {
	for _, kind := range target.Kinds {
		if !allowsKind(policy, kind) {
			return ErrForbidden
		}
	}
	if target.PrivateAddress && !policy.AllowPrivateTargets {
		return ErrForbidden
	}
	if target.Direction == model.DiagnosticDirectionReverse && !policy.AllowReverse {
		return ErrForbidden
	}
	if target.Protocol == model.DiagnosticProtocolUDP && !policy.AllowUDP {
		return ErrForbidden
	}
	if policy.DailyByteBudget > 0 && target.DailyByteBudget > policy.DailyByteBudget {
		return ErrForbidden
	}
	return nil
}

func (tx transaction) insertTarget(target model.DiagnosticTarget) error {
	kinds, allowed, err := encodeTargetLists(target)
	if err != nil {
		return err
	}
	return tx.one(`INSERT INTO diagnostic_targets(id, name, purpose, kinds_json, address, port, ip_family, protocol,
		direction, private_address, allowed_addresses_json, resolved_addresses_json, resolved_at,
		maximum_bitrate_mbps, maximum_duration_seconds, maximum_parallel_streams, daily_byte_budget, enabled,
		expires_at, revision, created_by, updated_by, created_at, updated_at)
	 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		target.ID, target.Name, target.Purpose, kinds, target.Address, target.Port, target.IPFamily, target.Protocol,
		target.Direction, target.PrivateAddress, allowed, encodeResolved(target.ResolvedAddresses), nullableTime(target.ResolvedAt),
		target.MaximumBitrateMbps, target.MaximumDuration, target.MaximumParallelStream, target.DailyByteBudget, target.Enabled,
		nullableTime(target.ExpiresAt), target.Revision, target.CreatedBy, target.UpdatedBy, target.CreatedAt, target.UpdatedAt)
}

func (tx transaction) updateTarget(target model.DiagnosticTarget) error {
	kinds, allowed, err := encodeTargetLists(target)
	if err != nil {
		return err
	}
	return tx.one(`UPDATE diagnostic_targets SET name=?, purpose=?, kinds_json=?, address=?, port=?, ip_family=?,
		protocol=?, direction=?, private_address=?, allowed_addresses_json=?, resolved_addresses_json=?, resolved_at=?,
		maximum_bitrate_mbps=?, maximum_duration_seconds=?, maximum_parallel_streams=?, daily_byte_budget=?, enabled=?,
		expires_at=?, revision=?, updated_by=?, updated_at=? WHERE id=? AND revision=?`,
		target.Name, target.Purpose, kinds, target.Address, target.Port, target.IPFamily, target.Protocol, target.Direction,
		target.PrivateAddress, allowed, encodeResolved(target.ResolvedAddresses), nullableTime(target.ResolvedAt),
		target.MaximumBitrateMbps, target.MaximumDuration, target.MaximumParallelStream, target.DailyByteBudget, target.Enabled,
		nullableTime(target.ExpiresAt), target.Revision, target.UpdatedBy, target.UpdatedAt, target.ID, target.Revision-1)
}

func encodeTargetLists(target model.DiagnosticTarget) (string, string, error) {
	kinds, err := json.Marshal(target.Kinds)
	if err != nil {
		return "", "", ErrInvalid
	}
	allowed, err := json.Marshal(target.AllowedAddresses)
	if err != nil {
		return "", "", ErrInvalid
	}
	return string(kinds), string(allowed), nil
}

func encodeResolved(addresses []string) string {
	if len(addresses) == 0 {
		return "[]"
	}
	encoded, _ := json.Marshal(addresses)
	return string(encoded)
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
