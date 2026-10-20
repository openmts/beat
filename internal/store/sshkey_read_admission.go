package store

import (
	"bytes"
	"context"
)

// 解密后、返回前复验会话和密文快照，审计失败时不得返回私钥。
func (s *SSHKeyStore) authorizeSSHKeyRead(ctx context.Context, id string, ciphertext []byte) error {
	if _, exists := ctx.Value(metadataActorKey{}).(MetadataActor); !exists {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return err
	}
	var name string
	var current []byte
	if err := tx.QueryRowContext(ctx, "SELECT name,private_key_encrypted FROM ssh_keys WHERE id=?", id).Scan(&name, &current); err != nil {
		return err
	}
	if !bytes.Equal(current, ciphertext) {
		return ErrAdminMutationConflict
	}
	if err := mutation.finish(metadataEvidence{Action: "ssh_key.reveal", Type: "ssh_key", ID: id, Name: name}); err != nil {
		return err
	}
	return tx.Commit()
}
