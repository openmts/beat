package remoteoperations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

const MaximumPolicies = 200

type PolicyInput struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Version  int      `json:"version"`
	Policy   Policy   `json:"policy"`
	NodeIDs  []string `json:"node_ids"`
	GroupIDs []string `json:"group_ids"`
}

func (service *Service) SavePolicy(ctx context.Context, actor Actor, input PolicyInput) (string, error) {
	if input.Policy.Validate() != nil || input.Name == "" || len(input.Name) > 200 || len(input.NodeIDs)+len(input.GroupIDs) > MaximumTargets {
		return "", ErrInvalid
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		return tx.savePolicy(actor, input)
	})
	return input.ID, err
}

func (tx transaction) savePolicy(actor Actor, input PolicyInput) error {
	encoded, err := json.Marshal(input.Policy)
	if err != nil {
		return ErrInvalid
	}
	var version int
	err = tx.tx.QueryRowContext(tx.ctx, "SELECT version FROM operation_policies WHERE id=?", input.ID).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrUnavailable
	}
	if version == 0 {
		var count int
		if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_policies").Scan(&count); err != nil {
			return ErrUnavailable
		}
		if count >= MaximumPolicies {
			return ErrForbidden
		}
	}
	if version != input.Version {
		return ErrConflict
	}
	if err := tx.one(`INSERT INTO operation_policies(id,name,enabled,configuration_json,version,creator_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,
		configuration_json=excluded.configuration_json,version=excluded.version,updated_at=excluded.updated_at`,
		input.ID, input.Name, input.Policy.Enabled, string(encoded), version+1, actor.UserID, tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
		return err
	}
	if err := tx.assignPolicy(input); err != nil {
		return err
	}
	return tx.audit(actor.UserID, "operation.policy.updated", input.ID)
}

func (tx transaction) assignPolicy(input PolicyInput) error {
	if err := tx.exec("DELETE FROM operation_policy_nodes WHERE policy_id=?", input.ID); err != nil {
		return err
	}
	if err := tx.exec("DELETE FROM operation_policy_groups WHERE policy_id=?", input.ID); err != nil {
		return err
	}
	for _, id := range input.NodeIDs {
		if err := tx.one("INSERT INTO operation_policy_nodes(policy_id,node_id) VALUES(?,?)", input.ID, id); err != nil {
			return err
		}
	}
	for _, id := range input.GroupIDs {
		if err := tx.one("INSERT INTO operation_policy_groups(policy_id,group_id) VALUES(?,?)", input.ID, id); err != nil {
			return err
		}
	}
	return nil
}

func (tx transaction) policy(id string) (Policy, error) {
	var encoded string
	var enabled bool
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT enabled,configuration_json FROM operation_policies WHERE id=?", id).Scan(&enabled, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return Policy{}, ErrNotFound
	}
	if err != nil {
		return Policy{}, ErrUnavailable
	}
	var policy Policy
	if json.Unmarshal([]byte(encoded), &policy) != nil || policy.Validate() != nil || !enabled || !policy.Enabled {
		return Policy{}, ErrForbidden
	}
	return policy, nil
}
