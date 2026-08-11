package agentcredential

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	tokenPrefix       = "beat_agent_v1_"
	randomBytes       = 32
	displayCharacters = 8
)

type Token struct {
	Plaintext string
	Prefix    string
	Hash      []byte
}

func Generate() (Token, error) {
	return GenerateFrom(rand.Reader)
}

func GenerateFrom(reader io.Reader) (Token, error) {
	random := make([]byte, randomBytes)
	if _, err := io.ReadFull(reader, random); err != nil {
		return Token{}, fmt.Errorf("generate agent token: %w", err)
	}
	plaintext := tokenPrefix + base64.RawURLEncoding.EncodeToString(random)
	prefix, ok := DisplayPrefix(plaintext)
	if !ok {
		return Token{}, errors.New("generate agent token: invalid generated token")
	}
	hash := Digest(plaintext)
	return Token{Plaintext: plaintext, Prefix: prefix, Hash: hash[:]}, nil
}

func Digest(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

func DisplayPrefix(token string) (string, bool) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return "", false
	}
	encoded := strings.TrimPrefix(token, tokenPrefix)
	random, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(random) != randomBytes || len(encoded) < displayCharacters {
		return "", false
	}
	return tokenPrefix + encoded[:displayCharacters], true
}

func Matches(hash []byte, token string) bool {
	if len(hash) != sha256.Size {
		return false
	}
	digest := Digest(token)
	return subtle.ConstantTimeCompare(hash, digest[:]) == 1
}
