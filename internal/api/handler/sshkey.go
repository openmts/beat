package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ssh"

	"github.com/beat/backend/internal/store"
)

type SSHKeyHandler struct {
	sshKeyStore *store.SSHKeyStore
}

func NewSSHKeyHandler(sshKeyStore *store.SSHKeyStore) *SSHKeyHandler {
	return &SSHKeyHandler{
		sshKeyStore: sshKeyStore,
	}
}

func (h *SSHKeyHandler) HandleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.sshKeyStore.ListSSHKeys(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list ssh keys")

		return
	}

	JSONResponse(w, http.StatusOK, keys)
}

func (h *SSHKeyHandler) HandleCreateSSHKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		PublicKey  string `json:"public_key"`
		PrivateKey string `json:"private_key"`
		KeyType    string `json:"key_type"`
	}
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if body.Name == "" || body.PublicKey == "" {
		JSONError(w, http.StatusBadRequest, "name and public_key are required")

		return
	}

	fingerprint := generateFingerprint(body.PublicKey)

	k, err := h.sshKeyStore.CreateSSHKey(r.Context(), body.Name, body.KeyType, body.PublicKey, body.PrivateKey, fingerprint)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to create ssh key")

		return
	}

	JSONResponse(w, http.StatusCreated, k)
}

func (h *SSHKeyHandler) HandleGenerateSSHKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		KeyType string `json:"key_type"`
	}
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if body.Name == "" {
		JSONError(w, http.StatusBadRequest, "name is required")

		return
	}

	if body.KeyType == "" {
		body.KeyType = "rsa"
	}

	var privateKeyPEM string
	var publicKeyStr string
	var fingerprint string
	var err error

	switch body.KeyType {
	case "rsa", "RSA":
		privateKeyPEM, publicKeyStr, fingerprint, err = generateRSAKey()
	case "ed25519", "Ed25519":
		privateKeyPEM, publicKeyStr, fingerprint, err = generateEd25519Key()
	default:
		JSONError(w, http.StatusBadRequest, "unsupported key type: "+body.KeyType)

		return
	}

	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to generate ssh key")

		return
	}

	k, err := h.sshKeyStore.CreateSSHKey(r.Context(), body.Name, body.KeyType, publicKeyStr, privateKeyPEM, fingerprint)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to store ssh key")

		return
	}

	JSONResponse(w, http.StatusCreated, generatedKeyResponse{
		ID:          k.ID,
		Name:        k.Name,
		KeyType:     k.KeyType,
		PublicKey:   k.PublicKey,
		PrivateKey:  privateKeyPEM,
		Fingerprint: k.Fingerprint,
		CreatedAt:   k.CreatedAt,
	})
}

type generatedKeyResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	KeyType     string    `json:"key_type"`
	PublicKey   string    `json:"public_key"`
	PrivateKey  string    `json:"private_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

func (h *SSHKeyHandler) HandleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.sshKeyStore.DeleteSSHKey(r.Context(), id); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to delete ssh key")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func generateFingerprint(publicKeyStr string) string {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKeyStr))
	if err != nil {
		hash := sha256.Sum256([]byte(publicKeyStr))

		return "SHA256:" + base64.RawStdEncoding.EncodeToString(hash[:])
	}

	hash := sha256.Sum256(pubKey.Marshal())

	return "SHA256:" + base64.RawStdEncoding.EncodeToString(hash[:])
}

func generateRSAKey() (string, string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", fmt.Errorf("generating rsa key: %w", err)
	}

	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", "", fmt.Errorf("converting rsa public key: %w", err)
	}

	publicKeyStr := string(ssh.MarshalAuthorizedKey(pub))

	fingerprint := generateFingerprint(publicKeyStr)

	privateKeyPEM := marshalRSAPrivateKey(privateKey)

	return privateKeyPEM, publicKeyStr, fingerprint, nil
}

func generateEd25519Key() (string, string, string, error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", fmt.Errorf("generating ed25519 key: %w", err)
	}

	pub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", "", fmt.Errorf("converting ed25519 public key: %w", err)
	}

	publicKeyStr := string(ssh.MarshalAuthorizedKey(pub))

	fingerprint := generateFingerprint(publicKeyStr)

	privateKeyPEM := marshalEd25519PrivateKey(privKey)

	return privateKeyPEM, publicKeyStr, fingerprint, nil
}

func marshalRSAPrivateKey(key *rsa.PrivateKey) string {
	der := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: der,
	}

	return string(pem.EncodeToMemory(block))
}

func marshalEd25519PrivateKey(key ed25519.PrivateKey) string {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return ""
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}

	return string(pem.EncodeToMemory(block))
}
