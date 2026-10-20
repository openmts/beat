package remoteoperations

import "github.com/beat/backend/internal/notificationqueue"

type guardedChunk struct {
	actor      Actor
	item       artifact
	transferID string
	index      int
}

type chunkReadOutcome struct {
	content []byte
	corrupt bool
}

func (tx transaction) readChunkMutation(spool *Spool, input guardedChunk) (chunkReadOutcome, *notificationqueue.Request, error) {
	content, err := tx.readChunk(spool, input.item, input.index)
	if err == errArtifactCorrupt {
		if _, err := tx.stopTransfer(input.actor, input.transferID, "corrupt"); err != nil {
			return chunkReadOutcome{}, nil, err
		}
		return chunkReadOutcome{corrupt: true}, operationFailure("artifact-corrupt:"+input.item.id, input.item.id), nil
	}
	return chunkReadOutcome{content: content}, nil, err
}
