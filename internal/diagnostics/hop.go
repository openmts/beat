package diagnostics

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

const maximumHopIdentityBytes = 512

// storeHopIdentities 以作业与跳索引为绑定写入加密身份；重复上报按索引覆盖。
func (tx transaction) storeHopIdentities(jobID string, identities []HopIdentity) error {
	if len(identities) > maximumFrameHops {
		return ErrInvalid
	}
	for _, identity := range identities {
		encoded, err := encodeHopIdentity(identity)
		if err != nil {
			return err
		}
		sealed, err := tx.access.Seal(encoded, secretbox.DiagnosticHopAAD(jobID, identity.Index))
		if err != nil {
			return ErrUnavailable
		}
		if err := tx.one(`INSERT INTO diagnostic_hop_identities(job_id, hop_index, identity_json, created_at)
			VALUES(?,?,?,?) ON CONFLICT(job_id, hop_index) DO UPDATE SET identity_json=excluded.identity_json`,
			jobID, identity.Index, string(sealed), tx.now); err != nil {
			return err
		}
	}
	return nil
}

func encodeHopIdentity(identity HopIdentity) ([]byte, error) {
	if identity.Index < 1 || identity.Index > model.MaximumDiagnosticHops {
		return nil, ErrInvalid
	}
	if !validHopText(identity.Address, model.MaximumDiagnosticAddressBytes) || !validHopText(identity.Hostname, model.MaximumDiagnosticNameLength) {
		return nil, ErrInvalid
	}
	encoded, err := json.Marshal(identity)
	if err != nil || len(encoded) > maximumHopIdentityBytes {
		return nil, ErrInvalid
	}
	return encoded, nil
}

func validHopText(value string, maximum int) bool {
	if len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

// hopIdentities 解密跳身份；密文不可认证时按存储不可用处理，不返回部分结果。
func (service *Service) hopIdentities(ctx context.Context, jobID string) ([]HopIdentity, error) {
	sealed, err := service.readHops(ctx, jobID)
	if err != nil {
		return nil, err
	}
	identities := make([]HopIdentity, 0, len(sealed))
	for _, hop := range sealed {
		plain, err := service.secrets.OpenResource(ctx, hop.ciphertext, secretbox.DiagnosticHopAAD(jobID, hop.index))
		if err != nil {
			return nil, ErrUnavailable
		}
		var identity HopIdentity
		if json.Unmarshal(plain, &identity) != nil || identity.Index != hop.index {
			return nil, ErrUnavailable
		}
		identities = append(identities, identity)
	}
	return identities, nil
}

type sealedHop struct {
	ciphertext []byte
	index      int
}

func (service *Service) readHops(ctx context.Context, jobID string) ([]sealedHop, error) {
	rows, err := service.db.QueryContext(ctx, `SELECT hop_index, identity_json FROM diagnostic_hop_identities
		WHERE job_id=? ORDER BY hop_index LIMIT ?`, jobID, model.MaximumDiagnosticHops)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = rows.Close() }()
	sealed := make([]sealedHop, 0, 8)
	for rows.Next() {
		var index int
		var ciphertext []byte
		if err := rows.Scan(&index, &ciphertext); err != nil {
			return nil, ErrUnavailable
		}
		if index < 1 || index > model.MaximumDiagnosticHops || len(ciphertext) == 0 {
			return nil, ErrUnavailable
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sealed = append(sealed, sealedHop{ciphertext: ciphertext, index: index})
	}
	if err := rows.Err(); err != nil {
		return nil, ErrUnavailable
	}
	return sealed, nil
}
