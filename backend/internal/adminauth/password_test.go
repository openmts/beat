package adminauth

import (
	"bytes"
	"errors"
	"testing"
)

func TestPasswordHasherRoundTrip(t *testing.T) {
	params := PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hasher := NewPasswordHasher(params, bytes.NewReader(bytes.Repeat([]byte{7}, 64)))
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	valid, err := hasher.Verify("correct horse battery staple", encoded)
	if err != nil || !valid {
		t.Fatalf("valid = %v, err = %v", valid, err)
	}
	valid, err = hasher.Verify("wrong password", encoded)
	if err != nil || valid {
		t.Fatalf("wrong password valid = %v, err = %v", valid, err)
	}
	if _, err := hasher.Verify("password", "invalid"); err == nil {
		t.Fatal("malformed hash accepted")
	}
}

func TestGenerateSessionToken(t *testing.T) {
	random := bytes.NewReader(bytes.Repeat([]byte{3}, 64))
	raw, hash, prefix, err := GenerateSessionToken(random)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if raw == "" || len(hash) != 32 || prefix == "" || raw == prefix {
		t.Fatalf("raw = %q, hash length = %d, prefix = %q", raw, len(hash), prefix)
	}
	if !SessionTokenMatches(raw, hash) {
		t.Fatal("generated token did not match hash")
	}
}

func TestPasswordParametersAndParsingErrors(t *testing.T) {
	defaults := DefaultPasswordParams()
	if err := defaults.validate(); err != nil {
		t.Fatalf("default password parameters: %v", err)
	}
	invalidParams := []PasswordParams{
		{MemoryKiB: 7, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16},
		{MemoryKiB: 8, Iterations: 0, Parallelism: 1, SaltLength: 16, KeyLength: 16},
		{MemoryKiB: 8, Iterations: 1, Parallelism: 0, SaltLength: 16, KeyLength: 16},
		{MemoryKiB: 8, Iterations: 1, Parallelism: 1, SaltLength: 15, KeyLength: 16},
		{MemoryKiB: 8, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 15},
	}
	for index, params := range invalidParams {
		if err := params.validate(); err == nil {
			t.Fatalf("invalid parameters %d accepted", index)
		}
	}

	hasher := NewPasswordHasher(invalidParams[0], bytes.NewReader(nil))
	if _, err := hasher.Hash("password"); err == nil {
		t.Fatal("invalid hash parameters accepted")
	}
	hasher = NewPasswordHasher(PasswordParams{
		MemoryKiB: 8, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16,
	}, errorPasswordReader{})
	if _, err := hasher.Hash("password"); err == nil {
		t.Fatal("password hashed with failed random source")
	}

	invalidHashes := []string{
		"$argon2i$v=19$m=8,t=1,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=8,t=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$x=8,t=1,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=no,t=1,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=8,x=1,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=8,t=no,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=8,t=1,x=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=8,t=1,p=no$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=8,t=1,p=1$***$AAAAAAAAAAAAAAAAAAAAAA",
		"$argon2id$v=19$m=8,t=1,p=1$AAAAAAAAAAAAAAAAAAAAAA$***",
	}
	for _, encoded := range invalidHashes {
		if _, _, _, err := parsePassword(encoded); err == nil {
			t.Fatalf("invalid password hash accepted: %q", encoded)
		}
	}
}

type errorPasswordReader struct{}

func (errorPasswordReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
