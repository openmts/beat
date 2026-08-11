package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type SSHKeyService struct {
	sshKeyStore *store.SSHKeyStore
}

func NewSSHKeyService(sshKeyStore *store.SSHKeyStore) *SSHKeyService {
	return &SSHKeyService{
		sshKeyStore: sshKeyStore,
	}
}

func (s *SSHKeyService) GenerateKeyPair(keyType string) (privateKey string, publicKey string, fingerprint string, err error) {
	switch keyType {
	case "rsa", "RSA":
		return s.generateRSAKeyPair()
	case "ed25519", "Ed25519":
		return s.generateEd25519KeyPair()
	default:
		return "", "", "", fmt.Errorf("unsupported key type: %s", keyType)
	}
}

func (s *SSHKeyService) generateRSAKeyPair() (string, string, string, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", fmt.Errorf("generate rsa key: %w", err)
	}

	pub, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		return "", "", "", fmt.Errorf("convert rsa public key: %w", err)
	}

	pubKeyStr := string(ssh.MarshalAuthorizedKey(pub))
	fingerprint := ssh.FingerprintSHA256(pub)

	der := x509.MarshalPKCS1PrivateKey(privKey)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: der,
	}
	privPEM := string(pem.EncodeToMemory(block))

	return privPEM, pubKeyStr, fingerprint, nil
}

func (s *SSHKeyService) generateEd25519KeyPair() (string, string, string, error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", fmt.Errorf("generate ed25519 key: %w", err)
	}

	pub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", "", fmt.Errorf("convert ed25519 public key: %w", err)
	}

	pubKeyStr := string(ssh.MarshalAuthorizedKey(pub))
	fingerprint := ssh.FingerprintSHA256(pub)

	der, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal ed25519 private key: %w", err)
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}
	privPEM := string(pem.EncodeToMemory(block))

	return privPEM, pubKeyStr, fingerprint, nil
}

func (s *SSHKeyService) CreateKey(ctx context.Context, name, keyType string) (*model.SSHKey, error) {
	privKey, pubKey, fingerprint, err := s.GenerateKeyPair(keyType)
	if err != nil {
		return nil, fmt.Errorf("generate key pair: %w", err)
	}

	key, err := s.sshKeyStore.CreateSSHKey(ctx, name, keyType, pubKey, privKey, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("create ssh key: %w", err)
	}

	return key, nil
}

func (s *SSHKeyService) ListKeys(ctx context.Context) ([]model.SSHKey, error) {
	keys, err := s.sshKeyStore.ListSSHKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ssh keys: %w", err)
	}
	return keys, nil
}

func (s *SSHKeyService) DeleteKey(ctx context.Context, id string) error {
	if err := s.sshKeyStore.DeleteSSHKey(ctx, id); err != nil {
		return fmt.Errorf("delete ssh key: %w", err)
	}
	return nil
}
