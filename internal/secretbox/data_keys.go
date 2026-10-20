package secretbox

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
)

type DataKey struct {
	ID       string `json:"-"`
	Material []byte `json:"-"`
}

func (DataKey) String() string { return "application data key" }

func (DataKey) GoString() string { return "application data key" }

type WrappedDataKey struct {
	ID         string `json:"-"`
	Generation int64  `json:"-"`
	Wrapped    []byte `json:"-"`
}

func (WrappedDataKey) String() string { return "wrapped application data key" }

func (WrappedDataKey) GoString() string { return "wrapped application data key" }

func GenerateDataKey(randomSource io.Reader) (DataKey, error) {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	value := make([]byte, dataKeyIDBytes+keyLength)
	if _, err := io.ReadFull(randomSource, value); err != nil {
		return DataKey{}, fmt.Errorf("generate data key: %w", err)
	}
	id := hex.EncodeToString(value[:dataKeyIDBytes])
	if _, err := parseDataKeyID(id); err != nil {
		clear(value)
		return DataKey{}, err
	}
	return DataKey{ID: id, Material: value[dataKeyIDBytes:]}, nil
}

func (manager *Manager) WrapDataKey(key DataKey, generation int64) ([]byte, error) {
	if _, err := parseDataKeyID(key.ID); err != nil {
		return nil, err
	}
	if len(key.Material) != keyLength || generation < 1 {
		return nil, errors.New("invalid wrapped data key")
	}
	return manager.rootCipher().Seal(key.Material, dataKeyAAD(key.ID, generation))
}

func (manager *Manager) UnwrapDataKey(key WrappedDataKey) (DataKey, error) {
	if _, err := parseDataKeyID(key.ID); err != nil {
		return DataKey{}, err
	}
	if key.Generation < 1 {
		return DataKey{}, errors.New("invalid wrapped data key generation")
	}
	material, err := manager.rootCipher().Open(key.Wrapped, dataKeyAAD(key.ID, key.Generation))
	if err != nil {
		return DataKey{}, err
	}
	if len(material) != keyLength {
		clear(material)
		return DataKey{}, errors.New("invalid wrapped data key size")
	}
	return DataKey{ID: key.ID, Material: material}, nil
}

func dataKeyAAD(id string, generation int64) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "application_data_key", RowID: id,
		Field: "wrapped_key", Revision: strconv.FormatInt(generation, 10), PayloadVersion: "v2"}
}

func (manager *Manager) rootCipher() *DataCipher {
	return &DataCipher{aead: manager.aead, id: manager.rootID, random: manager.random, mu: &manager.mu}
}

func (manager *Manager) RootFingerprint() string {
	return hex.EncodeToString(manager.rootFingerprint[:])
}
