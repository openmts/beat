package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"
)

const (
	MaximumSecretBytes = 1 << 20
	dataKeyIDBytes     = 16
	v2HeaderBytes      = 42
	v2NonceOffset      = 30
	v2LengthOffset     = 26
	v2NonceBytes       = 12
	v2TagBytes         = 16
	v2Magic            = "BEATENC2"
)

var ErrInvalidEnvelope = errors.New("application secret envelope is invalid")

type DataCipher struct {
	operationKey   []byte
	operationSpool cipher.AEAD
	aead           cipher.AEAD
	id             [dataKeyIDBytes]byte
	random         io.Reader
	mu             *sync.Mutex
}

func NewDataCipher(id string, material []byte, randomSource io.Reader) (*DataCipher, error) {
	identifier, err := parseDataKeyID(id)
	if err != nil {
		return nil, err
	}
	if len(material) != keyLength {
		return nil, errors.New("data keys must contain 32 bytes")
	}
	block, err := aes.NewCipher(material)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	operationKey, err := deriveOperationKey(material)
	if err != nil {
		return nil, err
	}
	operationSpool, err := deriveOperationSpoolCipher(material)
	if err != nil {
		return nil, err
	}
	return &DataCipher{operationSpool: operationSpool, aead: aead, id: identifier, random: randomSource, mu: new(sync.Mutex), operationKey: operationKey}, nil
}

func parseDataKeyID(value string) ([dataKeyIDBytes]byte, error) {
	var id [dataKeyIDBytes]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(id) || hex.EncodeToString(decoded) != value {
		return id, errors.New("invalid data key identifier")
	}
	copy(id[:], decoded)
	if id == [dataKeyIDBytes]byte{} {
		return id, errors.New("invalid data key identifier")
	}
	return id, nil
}

func (box *DataCipher) Seal(plaintext []byte, aad ResourceAAD) ([]byte, error) {
	if len(plaintext) > MaximumSecretBytes {
		return nil, errors.New("application secret exceeds the size limit")
	}
	encoded, err := resourceAAD(aad)
	if err != nil {
		return nil, err
	}
	header := make([]byte, v2HeaderBytes)
	copy(header, v2Magic)
	header[8], header[9] = 2, algorithmAESGCM
	copy(header[10:v2LengthOffset], box.id[:])
	binary.BigEndian.PutUint32(header[v2LengthOffset:v2NonceOffset], uint32(len(plaintext)+v2TagBytes))
	box.mu.Lock()
	_, err = io.ReadFull(box.random, header[v2NonceOffset:])
	box.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("generate resource nonce: %w", err)
	}
	return box.aead.Seal(header, header[v2NonceOffset:], plaintext, append(encoded, header...)), nil
}

func (box *DataCipher) Open(value []byte, aad ResourceAAD) ([]byte, error) {
	if err := validateV2Envelope(value); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(value[10:v2LengthOffset], box.id[:]) != 1 {
		return nil, ErrInvalidEnvelope
	}
	encoded, err := resourceAAD(aad)
	if err != nil {
		return nil, err
	}
	plaintext, err := box.aead.Open(nil, value[v2NonceOffset:v2HeaderBytes], value[v2HeaderBytes:], append(encoded, value[:v2HeaderBytes]...))
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	return plaintext, nil
}

func EnvelopeDataKeyID(value []byte) (string, error) {
	if err := validateV2Envelope(value); err != nil {
		return "", err
	}
	id := hex.EncodeToString(value[10:v2LengthOffset])
	if _, err := parseDataKeyID(id); err != nil {
		return "", ErrInvalidEnvelope
	}
	return id, nil
}

func validateV2Envelope(value []byte) error {
	if len(value) < v2HeaderBytes+v2TagBytes || len(value) > v2HeaderBytes+v2TagBytes+MaximumSecretBytes {
		return ErrInvalidEnvelope
	}
	if string(value[:8]) != v2Magic || value[8] != 2 || value[9] != algorithmAESGCM {
		return ErrInvalidEnvelope
	}
	length := binary.BigEndian.Uint32(value[v2LengthOffset:v2NonceOffset])
	if int(length) != len(value)-v2HeaderBytes || length < v2TagBytes {
		return ErrInvalidEnvelope
	}
	return nil
}

func IsV2Envelope(value []byte) bool {
	return len(value) >= len(v2Magic) && string(value[:len(v2Magic)]) == v2Magic
}

func resourceAAD(aad ResourceAAD) ([]byte, error) {
	if aad.Domain != "beat" || aad.Namespace == "" || aad.RowID == "" || aad.Field == "" || aad.PayloadVersion == "" {
		return nil, errors.New("application secret AAD is invalid")
	}
	for _, value := range []string{aad.Namespace, aad.RowID, aad.Field, aad.Revision, aad.PayloadVersion} {
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, errors.New("application secret AAD is invalid")
		}
	}
	return aad.Marshal()
}
