package remoteoperations

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

func (tx transaction) readChunk(spool *Spool, item artifact, index int) ([]byte, error) {
	var name, digest, keyID string
	var size int64
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT storage_name,digest,data_key_id,byte_size FROM operation_artifact_chunks WHERE artifact_id=? AND chunk_index=?", item.id, index).Scan(&name, &digest, &keyID, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errArtifactCorrupt
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	sealed, err := spool.read(name)
	if err != nil {
		return nil, errArtifactCorrupt
	}
	actualID, err := secretbox.OperationFrameKeyID(sealed)
	if err != nil || actualID != keyID {
		return nil, errArtifactCorrupt
	}
	access, ok := tx.access.(secretbox.OperationSpoolAccess)
	if !ok {
		return nil, errArtifactCorrupt
	}
	plain, err := access.OpenOperationFrame(sealed, secretbox.OperationFrame{ArtifactID: item.id, Direction: item.direction, Index: index, TotalBytes: item.size, Digest: item.digest})
	if err != nil {
		return nil, errArtifactCorrupt
	}
	hash := sha256.Sum256(plain)
	if int64(len(plain)) != size || digest != hex.EncodeToString(hash[:]) {
		clear(plain)
		return nil, errArtifactCorrupt
	}
	return plain, nil
}
