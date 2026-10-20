package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

func (service *ServicePrincipalStore) loadConstraints(ctx context.Context, value *model.ServicePrincipal) error {
	scopes, err := loadServicePrincipalScopes(ctx, service.db, value.ID)
	if err != nil {
		return err
	}
	resources, err := loadServicePrincipalResources(ctx, service.db, value.ID)
	if err != nil {
		return err
	}
	value.Scopes = scopes
	value.Resources = resources
	return nil
}

func loadServicePrincipalScopes(ctx context.Context, database schemaExecutor, id string) ([]model.ServiceScope, error) {
	rows, err := database.QueryContext(ctx, "SELECT scope FROM service_token_scopes WHERE principal_id = ? ORDER BY scope", id)
	if err != nil {
		return nil, fmt.Errorf("load service token scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	scopes := []model.ServiceScope{}
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("read service token scope: %w", err)
		}
		scopes = append(scopes, model.ServiceScope(scope))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service token scopes: %w", err)
	}
	return scopes, nil
}

func loadServicePrincipalResources(ctx context.Context, database schemaExecutor, id string) ([]model.ServiceResource, error) {
	rows, err := database.QueryContext(ctx, "SELECT kind, resource_id FROM service_token_resources WHERE principal_id = ? ORDER BY kind, resource_id", id)
	if err != nil {
		return nil, fmt.Errorf("load service token resources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	resources := []model.ServiceResource{}
	for rows.Next() {
		var kind, resourceID string
		if err := rows.Scan(&kind, &resourceID); err != nil {
			return nil, fmt.Errorf("read service token resource: %w", err)
		}
		resources = append(resources, model.ServiceResource{Kind: model.ServiceResourceKind(kind), ID: resourceID})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service token resources: %w", err)
	}
	return resources, nil
}

func scanServicePrincipal(scanner rowScanner) (*model.ServicePrincipal, error) {
	var value model.ServicePrincipal
	var visibility, sources string
	var revoked, superseded, lastUsed sql.NullTime
	err := scanner.Scan(&value.ID, &value.Name, &value.Purpose, &value.TokenPrefix, &value.TokenHash,
		&visibility, &sources, &value.ExpiresAt, &revoked, &value.RevokedReason, &superseded,
		&value.RotationOverlapSeconds, &value.RotatedFrom, &value.Revision, &value.CreatedBy,
		&value.CreatedAt, &lastUsed)
	if err != nil {
		return nil, err
	}
	value.Visibility = model.ServiceVisibility(visibility)
	if err := json.Unmarshal([]byte(sources), &value.SourceCIDRs); err != nil {
		return nil, fmt.Errorf("decode service token sources: %w", err)
	}
	value.RevokedAt = nullableTime(revoked)
	value.SupersededAt = nullableTime(superseded)
	value.LastUsedAt = nullableTime(lastUsed)
	value.Scopes = []model.ServiceScope{}
	value.Resources = []model.ServiceResource{}
	return &value, nil
}

func readServicePrincipalIdentity(ctx context.Context, database schemaExecutor, id string) (string, string, error) {
	var name, prefix string
	err := database.QueryRowContext(ctx, "SELECT name, token_prefix FROM service_principals WHERE id = ?", id).Scan(&name, &prefix)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrServicePrincipalNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("read service token: %w", err)
	}
	return name, prefix, nil
}
