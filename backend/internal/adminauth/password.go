package adminauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  int
	KeyLength   uint32
}

type PasswordHasher struct {
	params PasswordParams
	random io.Reader
}

func DefaultPasswordParams() PasswordParams {
	return PasswordParams{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32}
}

func NewPasswordHasher(params PasswordParams, randomSource io.Reader) *PasswordHasher {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &PasswordHasher{params: params, random: randomSource}
}

func (hasher *PasswordHasher) Hash(password string) (string, error) {
	if err := hasher.params.validate(); err != nil {
		return "", err
	}
	salt := make([]byte, hasher.params.SaltLength)
	if _, err := io.ReadFull(hasher.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, hasher.params.Iterations,
		hasher.params.MemoryKiB, hasher.params.Parallelism, hasher.params.KeyLength)
	return encodePassword(hasher.params, salt, hash), nil
}

func (hasher *PasswordHasher) Verify(password, encoded string) (bool, error) {
	params, salt, expected, err := parsePassword(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations,
		params.MemoryKiB, params.Parallelism, params.KeyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func (params PasswordParams) validate() error {
	if params.MemoryKiB < 8 || params.Iterations == 0 || params.Parallelism == 0 ||
		params.SaltLength < 16 || params.KeyLength < 16 {
		return errors.New("Argon2id parameters are invalid")
	}
	return nil
}

func encodePassword(params PasswordParams, salt, hash []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		params.MemoryKiB, params.Iterations, params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
}

func parsePassword(encoded string) (PasswordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return PasswordParams{}, nil, nil, errors.New("encoded password hash is invalid")
	}
	params, err := parsePasswordParams(parts[3])
	if err != nil {
		return PasswordParams{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return PasswordParams{}, nil, nil, errors.New("encoded password salt is invalid")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return PasswordParams{}, nil, nil, errors.New("encoded password hash is invalid")
	}
	params.SaltLength = len(salt)
	params.KeyLength = uint32(len(hash))
	if err := params.validate(); err != nil {
		return PasswordParams{}, nil, nil, err
	}
	return params, salt, hash, nil
}

func parsePasswordParams(value string) (PasswordParams, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return PasswordParams{}, errors.New("encoded Argon2id parameters are invalid")
	}
	memory, err := parseUintParam(parts[0], "m=", 32)
	if err != nil {
		return PasswordParams{}, err
	}
	iterations, err := parseUintParam(parts[1], "t=", 32)
	if err != nil {
		return PasswordParams{}, err
	}
	parallelism, err := parseUintParam(parts[2], "p=", 8)
	if err != nil {
		return PasswordParams{}, err
	}
	return PasswordParams{MemoryKiB: uint32(memory), Iterations: uint32(iterations), Parallelism: uint8(parallelism)}, nil
}

func parseUintParam(value, prefix string, bits int) (uint64, error) {
	if !strings.HasPrefix(value, prefix) {
		return 0, errors.New("encoded Argon2id parameters are invalid")
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, bits)
	if err != nil {
		return 0, errors.New("encoded Argon2id parameters are invalid")
	}
	return parsed, nil
}
