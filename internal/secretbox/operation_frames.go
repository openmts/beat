package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
)

const OperationFrameBytes = 4 * 1024 * 1024

const operationFrameHeaderBytes = 8 + dataKeyIDBytes + v2NonceBytes

const operationFrameMagic = "BEATOPS1"

type OperationFrame struct {
	ArtifactID string `json:"artifact_id"`
	Direction  string `json:"direction"`
	Index      int    `json:"index"`
	TotalBytes int64  `json:"total_bytes"`
	Digest     string `json:"digest"`
}

type OperationSpoolAccess interface {
	SealOperationFrame([]byte, OperationFrame) ([]byte, error)
	OpenOperationFrame([]byte, OperationFrame) ([]byte, error)
}

func deriveOperationSpoolCipher(material []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, material, nil, "beat/operations/spool/v1", sha256.Size)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (frame OperationFrame) aad() ([]byte, error) {
	if frame.ArtifactID == "" || len(frame.ArtifactID) > 128 || (frame.Direction != "deploy" && frame.Direction != "collect") || frame.Index < 0 || frame.Index >= 4096 || frame.TotalBytes < 0 || frame.TotalBytes > 2*1024*1024*1024 {
		return nil, ErrInvalidEnvelope
	}
	digest, err := hex.DecodeString(frame.Digest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != frame.Digest {
		return nil, ErrInvalidEnvelope
	}
	return json.Marshal(frame)
}

func (box *DataCipher) SealOperationFrame(plain []byte, frame OperationFrame) ([]byte, error) {
	if len(plain) > OperationFrameBytes {
		return nil, ErrInvalidEnvelope
	}
	aad, err := frame.aad()
	if err != nil {
		return nil, err
	}
	out := make([]byte, operationFrameHeaderBytes)
	copy(out, operationFrameMagic)
	copy(out[8:], box.id[:])
	nonce := out[8+dataKeyIDBytes:]
	box.mu.Lock()
	_, err = io.ReadFull(box.random, nonce)
	box.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return box.operationSpool.Seal(out, nonce, plain, aad), nil
}

func (box *DataCipher) OpenOperationFrame(sealed []byte, frame OperationFrame) ([]byte, error) {
	id, err := OperationFrameKeyID(sealed)
	if err != nil || id != hex.EncodeToString(box.id[:]) {
		return nil, ErrInvalidEnvelope
	}
	aad, err := frame.aad()
	if err != nil {
		return nil, err
	}
	plain, err := box.operationSpool.Open(nil, sealed[8+dataKeyIDBytes:operationFrameHeaderBytes], sealed[operationFrameHeaderBytes:], aad)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	return plain, nil
}

func OperationFrameKeyID(sealed []byte) (string, error) {
	if len(sealed) < operationFrameHeaderBytes+v2TagBytes || len(sealed) > operationFrameHeaderBytes+v2TagBytes+OperationFrameBytes || string(sealed[:8]) != operationFrameMagic {
		return "", ErrInvalidEnvelope
	}
	id := hex.EncodeToString(sealed[8 : 8+dataKeyIDBytes])
	if _, err := parseDataKeyID(id); err != nil {
		return "", ErrInvalidEnvelope
	}
	return id, nil
}
