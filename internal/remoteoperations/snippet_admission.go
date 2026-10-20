package remoteoperations

import (
	"bytes"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

func (tx transaction) validateTaskSnippet(creation *taskCreation) error {
	input := creation.input
	if input.Limits.Kind != "snippet" {
		if input.SnippetID != "" || input.SnippetVersion != 0 {
			return ErrInvalid
		}
		return nil
	}
	var ciphertext []byte
	var shell string
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT v.content_encrypted,v.shell_family FROM operation_snippet_versions v
		JOIN operation_snippets s ON s.id=v.snippet_id WHERE s.id=? AND v.version=? AND v.approved_at IS NOT NULL AND s.visibility='shared' AND s.deleted_at IS NULL`, input.SnippetID, input.SnippetVersion).Scan(&ciphertext, &shell)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return ErrUnavailable
	}
	plain, err := tx.access.Open(ciphertext, secretbox.OperationSnippetAAD(input.SnippetID, input.SnippetVersion, shell))
	if err != nil {
		return ErrUnavailable
	}
	defer clear(plain)
	if shell != input.ShellFamily || !bytes.Equal(plain, input.Command) {
		return ErrConflict
	}
	return nil
}
