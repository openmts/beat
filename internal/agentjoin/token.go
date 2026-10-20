package agentjoin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const tokenRandomBytes = 32

const tokenDisplayCharacters = 8

type Token struct {
	Plaintext string
	Hash      []byte
	Prefix    string
}

func GenerateToken() (Token, error) { return GenerateTokenFrom(rand.Reader) }

func GenerateTokenFrom(reader io.Reader) (Token, error) {
	random := make([]byte, tokenRandomBytes)
	if _, err := io.ReadFull(reader, random); err != nil {
		return Token{}, fmt.Errorf("generate join credential: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(random)
	plaintext := TokenPrefix + encoded
	return Token{Plaintext: plaintext, Hash: DigestToken(plaintext), Prefix: TokenPrefix + encoded[:tokenDisplayCharacters]}, nil
}

func ValidateToken(value string) error {
	if !strings.HasPrefix(value, TokenPrefix) {
		return errors.New("invalid join credential")
	}
	encoded := strings.TrimPrefix(value, TokenPrefix)
	random, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(random) != tokenRandomBytes || base64.RawURLEncoding.EncodeToString(random) != encoded {
		return errors.New("invalid join credential")
	}
	return nil
}

func DigestToken(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

func JoinCommand(serverURL, token string) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	command := "sudo ./beat-agent join --server " + quote(serverURL) + " --token " + quote(token)
	if strings.HasPrefix(serverURL, "http://") {
		command += " --allow-http"
	}
	return command
}
