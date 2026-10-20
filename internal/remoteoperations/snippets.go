package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

type SnippetInput struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Visibility      string `json:"visibility"`
	ShellFamily     string `json:"shell_family"`
	Content         []byte `json:"content"`
	ExpectedVersion int    `json:"expected_version"`
}

type SnippetVersion struct {
	OwnerID     string `json:"owner_id"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Version     int    `json:"version"`
	ShellFamily string `json:"shell_family"`
	Fingerprint string `json:"fingerprint"`
	Approved    bool   `json:"approved"`
}

func (service *Service) SaveSnippet(ctx context.Context, actor Actor, input SnippetInput) (SnippetVersion, error) {
	if input.Name == "" || len(input.Name) > 200 || len(input.Description) > 2000 || ValidateCommand(input.Content) != nil {
		return SnippetVersion{}, ErrInvalid
	}
	if !slices.Contains([]string{"private", "shared"}, input.Visibility) || !slices.Contains([]string{"sh", "powershell"}, input.ShellFamily) {
		return SnippetVersion{}, ErrInvalid
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	var version SnippetVersion
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		version, err = tx.saveSnippet(actor, input)
		return err
	})
	return version, err
}

func (tx transaction) saveSnippet(actor Actor, input SnippetInput) (SnippetVersion, error) {
	var owner string
	var current int
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT owner_id,current_version FROM operation_snippets WHERE id=? AND deleted_at IS NULL", input.ID).Scan(&owner, &current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SnippetVersion{}, ErrUnavailable
	}
	if owner != "" && owner != actor.UserID {
		return SnippetVersion{}, ErrForbidden
	}
	if input.ExpectedVersion != current {
		return SnippetVersion{}, ErrConflict
	}
	version := SnippetVersion{OwnerID: actor.UserID, ID: input.ID, Name: input.Name, Description: input.Description, Visibility: input.Visibility, Version: current + 1, ShellFamily: input.ShellFamily}
	sealed, err := tx.access.Seal(input.Content, secretbox.OperationSnippetAAD(input.ID, version.Version, input.ShellFamily))
	if err != nil {
		return SnippetVersion{}, ErrUnavailable
	}
	version.Fingerprint, err = fingerprint(tx.access, input.Content)
	if err != nil {
		return SnippetVersion{}, err
	}
	if err := tx.one(`INSERT INTO operation_snippets(id,name,description,visibility,owner_id,current_version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,visibility=excluded.visibility,current_version=excluded.current_version,updated_at=excluded.updated_at`,
		input.ID, input.Name, input.Description, input.Visibility, actor.UserID, version.Version, tx.now.UnixMilli(), tx.now.UnixMilli()); err != nil {
		return SnippetVersion{}, err
	}
	if err := tx.one(`INSERT INTO operation_snippet_versions(id,snippet_id,version,shell_family,content_encrypted,fingerprint,created_at) VALUES(?,?,?,?,?,?,?)`,
		uuid.NewString(), input.ID, version.Version, input.ShellFamily, sealed, version.Fingerprint, tx.now.UnixMilli()); err != nil {
		return SnippetVersion{}, err
	}
	return version, tx.audit(actor.UserID, "operation.snippet.saved", input.ID)
}

func (service *Service) ApproveSnippet(ctx context.Context, actor Actor, input SnippetVersion) error {
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		result, err := tx.tx.ExecContext(tx.ctx, `UPDATE operation_snippet_versions SET approved_by=?,approved_at=? WHERE snippet_id=? AND version=?
			AND EXISTS(SELECT 1 FROM operation_snippets WHERE id=? AND current_version=? AND visibility='shared' AND deleted_at IS NULL)`, actor.UserID, tx.now.UnixMilli(), input.ID, input.Version, input.ID, input.Version)
		if err != nil {
			return ErrUnavailable
		}
		count, err := result.RowsAffected()
		if err != nil {
			return ErrUnavailable
		}
		if count != 1 {
			return ErrConflict
		}
		return tx.audit(actor.UserID, "operation.snippet.approved", input.ID)
	})
}

func (service *Service) SnippetContent(ctx context.Context, actor Actor, input SnippetVersion) ([]byte, error) {
	var content []byte
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		var sealed []byte
		var shell string
		err := tx.tx.QueryRowContext(tx.ctx, `SELECT v.content_encrypted,v.shell_family FROM operation_snippet_versions v JOIN operation_snippets s ON s.id=v.snippet_id
			WHERE s.id=? AND v.version=? AND s.deleted_at IS NULL AND (s.owner_id=? OR s.visibility='shared')`, input.ID, input.Version, actor.UserID).Scan(&sealed, &shell)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return ErrUnavailable
		}
		content, err = tx.access.Open(sealed, secretbox.OperationSnippetAAD(input.ID, input.Version, shell))
		if err != nil {
			return ErrUnavailable
		}
		return tx.audit(actor.UserID, "operation.snippet.viewed", input.ID)
	})
	if err != nil {
		clear(content)
		return nil, err
	}
	return content, nil
}

func (service *Service) ListSnippets(ctx context.Context, actor Actor) ([]SnippetVersion, error) {
	var versions []SnippetVersion
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		versions, err = tx.listSnippets(actor)
		return err
	})
	return versions, err
}

func (tx transaction) listSnippets(actor Actor) (versions []SnippetVersion, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT s.id,s.owner_id,s.name,s.description,s.visibility,v.version,v.shell_family,v.fingerprint,v.approved_at IS NOT NULL
		FROM operation_snippets s JOIN operation_snippet_versions v ON v.snippet_id=s.id AND v.version=s.current_version
		WHERE s.deleted_at IS NULL AND (s.owner_id=? OR s.visibility='shared') ORDER BY s.name,s.id LIMIT 200`, actor.UserID)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	versions = []SnippetVersion{}
	for rows.Next() {
		var item SnippetVersion
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Name, &item.Description, &item.Visibility, &item.Version, &item.ShellFamily, &item.Fingerprint, &item.Approved); err != nil {
			return nil, ErrUnavailable
		}
		versions = append(versions, item)
	}
	return versions, rows.Err()
}
