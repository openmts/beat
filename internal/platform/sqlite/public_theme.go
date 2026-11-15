package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrPublicThemeNotFound = errors.New("public theme revision not found")

type PublicThemeRevision struct {
	Revision     int64
	Status       string
	DocumentJSON []byte
	CreatedAt    time.Time
	PublishedAt  *time.Time
}

func (s *Store) GetPublicThemeRevision(ctx context.Context, status string) (PublicThemeRevision, error) {
	return scanPublicThemeRevision(s.db.QueryRowContext(ctx, `
		SELECT revision, status, document_json, created_at, published_at
		FROM public_theme_revisions
		WHERE status = ?
		ORDER BY revision DESC
		LIMIT 1`, status))
}

func (s *Store) SavePublicThemeDraft(ctx context.Context, documentJSON []byte, now time.Time) (PublicThemeRevision, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PublicThemeRevision{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE public_theme_revisions SET status = 'archived' WHERE status = 'draft'`); err != nil {
		return PublicThemeRevision{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO public_theme_revisions (status, document_json, created_at)
		VALUES ('draft', ?, ?)`, string(documentJSON), now.Unix())
	if err != nil {
		return PublicThemeRevision{}, err
	}
	revision, err := result.LastInsertId()
	if err != nil {
		return PublicThemeRevision{}, err
	}
	if err := tx.Commit(); err != nil {
		return PublicThemeRevision{}, err
	}
	return PublicThemeRevision{Revision: revision, Status: "draft", DocumentJSON: documentJSON, CreatedAt: now}, nil
}

func (s *Store) PublishPublicThemeDraft(ctx context.Context, now time.Time) (PublicThemeRevision, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PublicThemeRevision{}, err
	}
	defer tx.Rollback()
	draft, err := scanPublicThemeRevision(tx.QueryRowContext(ctx, `
		SELECT revision, status, document_json, created_at, published_at
		FROM public_theme_revisions
		WHERE status = 'draft'
		ORDER BY revision DESC
		LIMIT 1`))
	if err != nil {
		return PublicThemeRevision{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public_theme_revisions SET status = 'archived' WHERE status = 'published'`); err != nil {
		return PublicThemeRevision{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public_theme_revisions
		SET status = 'published', published_at = ?
		WHERE revision = ? AND status = 'draft'`, now.Unix(), draft.Revision); err != nil {
		return PublicThemeRevision{}, err
	}
	if err := tx.Commit(); err != nil {
		return PublicThemeRevision{}, err
	}
	draft.Status = "published"
	draft.PublishedAt = &now
	return draft, nil
}

func (s *Store) ListPublicThemeRevisions(ctx context.Context, limit int) ([]PublicThemeRevision, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT revision, status, document_json, created_at, published_at
		FROM public_theme_revisions
		ORDER BY revision DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	revisions := make([]PublicThemeRevision, 0, limit)
	for rows.Next() {
		revision, err := scanPublicThemeRevision(rows)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

type publicThemeScanner interface {
	Scan(dest ...any) error
}

func scanPublicThemeRevision(scanner publicThemeScanner) (PublicThemeRevision, error) {
	var revision PublicThemeRevision
	var documentJSON string
	var createdAt int64
	var publishedAt sql.NullInt64
	err := scanner.Scan(&revision.Revision, &revision.Status, &documentJSON, &createdAt, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicThemeRevision{}, ErrPublicThemeNotFound
	}
	if err != nil {
		return PublicThemeRevision{}, err
	}
	revision.DocumentJSON = []byte(documentJSON)
	revision.CreatedAt = time.Unix(createdAt, 0).UTC()
	if publishedAt.Valid {
		value := time.Unix(publishedAt.Int64, 0).UTC()
		revision.PublishedAt = &value
	}
	return revision, nil
}
