package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
)

func (service *Service) GetPolicy(ctx context.Context, actor Actor, id string) (PolicyInput, error) {
	policies, err := service.ListPolicies(ctx, actor)
	if err != nil {
		return PolicyInput{}, err
	}
	for _, policy := range policies {
		if policy.ID == id {
			return policy, nil
		}
	}
	return PolicyInput{}, ErrNotFound
}

func (service *Service) DisablePolicy(ctx context.Context, actor Actor, id string) error {
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		var exists int
		if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_policies WHERE id=?", id).Scan(&exists); err != nil {
			return ErrUnavailable
		}
		if exists != 1 {
			return ErrNotFound
		}
		if err := tx.one("UPDATE operation_policies SET enabled=0,configuration_json=json_set(configuration_json,'$.enabled',json('false')),version=version+1,updated_at=? WHERE id=?", tx.now.UnixMilli(), id); err != nil {
			return err
		}
		return tx.audit(actor.UserID, "operation.policy.disabled", id)
	})
}

func (service *Service) DeleteSnippet(ctx context.Context, actor Actor, id string) error {
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var owner string
		err := tx.tx.QueryRowContext(tx.ctx, "SELECT owner_id FROM operation_snippets WHERE id=? AND deleted_at IS NULL", id).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return ErrUnavailable
		}
		if owner != actor.UserID {
			return ErrForbidden
		}
		if err := tx.one("UPDATE operation_snippets SET deleted_at=?,updated_at=? WHERE id=?", tx.now.UnixMilli(), tx.now.UnixMilli(), id); err != nil {
			return err
		}
		return tx.audit(actor.UserID, "operation.snippet.deleted", id)
	})
}
