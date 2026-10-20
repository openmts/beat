package remoteoperations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

type TaskContent struct {
	ID      string    `json:"id"`
	Payload TaskInput `json:"payload"`
}

func (service *Service) TaskContent(ctx context.Context, actor Actor, id string) (TaskContent, error) {
	var content TaskContent
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		var sealed []byte
		err := tx.tx.QueryRowContext(tx.ctx, "SELECT payload_encrypted FROM operation_tasks WHERE id=?", id).Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return ErrUnavailable
		}
		plain, err := tx.access.Open(sealed, secretbox.OperationTaskAAD(id))
		if err != nil {
			return ErrUnavailable
		}
		defer clear(plain)
		if json.Unmarshal(plain, &content.Payload) != nil {
			return ErrUnavailable
		}
		content.ID = id
		return tx.audit(actor.UserID, "operation.command.viewed", id)
	})
	if err != nil {
		clear(content.Payload.Command)
		return TaskContent{}, err
	}
	return content, nil
}
