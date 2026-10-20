package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

func validateServicePrincipal(value model.ServicePrincipal) error {
	if value.ID == "" || len(value.ID) > model.MaximumServiceTokenIDLength {
		return errors.New("service token identity is invalid")
	}
	if value.Name == "" || len(value.Name) > model.MaximumServiceTokenName {
		return errors.New("service token name is invalid")
	}
	if len(value.Purpose) > model.MaximumServiceTokenPurpose {
		return errors.New("service token purpose is invalid")
	}
	if !strings.HasPrefix(value.TokenPrefix, model.ServiceTokenPrefix) || len(value.TokenPrefix) > 64 {
		return errors.New("service token prefix is invalid")
	}
	if len(value.TokenHash) != 32 {
		return errors.New("service token hash is invalid")
	}
	if !value.Visibility.Valid() {
		return errors.New("service token visibility is invalid")
	}
	if len(value.Scopes) == 0 {
		return errors.New("service token scopes are empty")
	}
	for _, scope := range value.Scopes {
		if !scope.Valid() {
			return errors.New("service token scope is invalid")
		}
	}
	for _, resource := range value.Resources {
		if !resource.Kind.Valid() || resource.ID == "" || len(resource.ID) > model.MaximumServiceTokenIDLength {
			return errors.New("service token resource is invalid")
		}
	}
	if len(value.SourceCIDRs) > model.MaximumServiceTokenCIDRs {
		return errors.New("service token source restriction is invalid")
	}
	if value.ExpiresAt.IsZero() || value.CreatedAt.IsZero() || value.CreatedBy == "" {
		return errors.New("service token metadata is invalid")
	}
	if value.Revision < 1 {
		return errors.New("service token revision is invalid")
	}
	return nil
}

func insertServicePrincipal(ctx context.Context, tx *sql.Tx, value model.ServicePrincipal) error {
	sources, err := json.Marshal(value.SourceCIDRs)
	if err != nil {
		return fmt.Errorf("encode service token sources: %w", err)
	}
	if value.Revision < 1 {
		value.Revision = 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO service_principals(
		id, name, purpose, token_prefix, token_hash, visibility, source_cidrs, expires_at,
		revoked_at, revoked_reason, superseded_at, rotation_overlap_seconds, rotated_from,
		revision, created_by, created_at, last_used_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ID, value.Name, value.Purpose, value.TokenPrefix, value.TokenHash, string(value.Visibility),
		string(sources), value.ExpiresAt.UTC(), nullableServiceTime(value.RevokedAt), value.RevokedReason,
		nullableServiceTime(value.SupersededAt), value.RotationOverlapSeconds, nullIfEmpty(value.RotatedFrom),
		value.Revision, value.CreatedBy, value.CreatedAt.UTC(), nullableServiceTime(value.LastUsedAt)); err != nil {
		return fmt.Errorf("insert service token: %w", err)
	}
	return insertServicePrincipalConstraints(ctx, tx, value)
}

func insertServicePrincipalConstraints(ctx context.Context, tx *sql.Tx, value model.ServicePrincipal) error {
	for _, scope := range value.Scopes {
		if _, err := tx.ExecContext(ctx, "INSERT INTO service_token_scopes(principal_id, scope) VALUES(?,?)", value.ID, string(scope)); err != nil {
			return fmt.Errorf("insert service token scope: %w", err)
		}
	}
	for _, resource := range value.Resources {
		if _, err := tx.ExecContext(ctx, "INSERT INTO service_token_resources(principal_id, kind, resource_id) VALUES(?,?,?)",
			value.ID, string(resource.Kind), resource.ID); err != nil {
			return fmt.Errorf("insert service token resource: %w", err)
		}
	}
	return nil
}

func rotateServicePrincipal(ctx context.Context, tx *sql.Tx, change ServicePrincipalRotation) error {
	previous, next := change.Previous, change.Next
	var revoked sql.NullTime
	err := tx.QueryRowContext(ctx, "SELECT revoked_at FROM service_principals WHERE id = ?", previous).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrServicePrincipalNotFound
	}
	if err != nil {
		return fmt.Errorf("read service token predecessor: %w", err)
	}
	if revoked.Valid {
		return ErrServicePrincipalRevoked
	}
	if err := insertServicePrincipal(ctx, tx, next); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE service_principals
		SET superseded_at = ?, rotation_overlap_seconds = ?, revision = revision + 1
		WHERE id = ? AND revoked_at IS NULL`, change.At.UTC(), int(change.Overlap.Seconds()), previous)
	if err != nil {
		return fmt.Errorf("supersede service token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("supersede service token: %w", err)
	}
	if affected != 1 {
		return ErrServicePrincipalRotation
	}
	return nil
}

func classifyServicePrincipalMutation(ctx context.Context, tx *sql.Tx, id string) error {
	var revoked sql.NullTime
	if err := tx.QueryRowContext(ctx, "SELECT revoked_at FROM service_principals WHERE id = ?", id).Scan(&revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrServicePrincipalNotFound
		}
		return fmt.Errorf("read service token: %w", err)
	}
	if revoked.Valid {
		return ErrServicePrincipalRevoked
	}
	return ErrServicePrincipalNotFound
}

func nullableServiceTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
