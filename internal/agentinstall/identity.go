package agentinstall

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/beat/backend/internal/agentenrollment"
)

// identityVersion 是本地入网密钥文件格式版本。
const identityVersion = 1

// Identity 是 Agent 在本机持有的 Ed25519 入网密钥对；私钥只以 0600 文件落盘。
type Identity struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// Fingerprint 返回密钥指纹，即服务端用于识别同一台机器的稳定标识。
func (identity Identity) Fingerprint() []byte {
	sum := agentenrollment.Fingerprint(identity.PublicKey)
	return sum[:]
}

type identityFile struct {
	Version    int    `json:"version"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// loadOrCreateIdentity 读取本地密钥，缺失时生成并立即落盘，避免请求令牌与密钥分离。
func (installer *Installer) loadOrCreateIdentity() (Identity, error) {
	path := filepath.Join(installer.paths.stateDir, "identity.json")
	var record identityFile
	found, err := loadPrivateJSON(path, &record)
	if err != nil {
		return Identity{}, err
	}
	if found {
		return decodeIdentity(record)
	}
	return installer.createIdentity(path)
}

func (installer *Installer) createIdentity(path string) (Identity, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(installer.random)
	if err != nil {
		return Identity{}, fmt.Errorf("generate enrollment key: %w", err)
	}
	record := identityFile{Version: identityVersion,
		PublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey)}
	encoded, err := json.Marshal(record)
	if err != nil {
		return Identity{}, fmt.Errorf("encode enrollment key: %w", err)
	}
	if err := installer.write(path, encoded, privateFile); err != nil {
		return Identity{}, fmt.Errorf("save enrollment key: %w", err)
	}
	return Identity{PublicKey: publicKey, PrivateKey: privateKey}, nil
}

func decodeIdentity(record identityFile) (Identity, error) {
	if record.Version != identityVersion {
		return Identity{}, errors.New("stored enrollment key has an unsupported version; remove it before enrolling again")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(record.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return Identity{}, errors.New("stored enrollment key is invalid; remove it before enrolling again")
	}
	privateKey, err := base64.RawURLEncoding.DecodeString(record.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return Identity{}, errors.New("stored enrollment key is invalid; remove it before enrolling again")
	}
	return Identity{PublicKey: publicKey, PrivateKey: privateKey}, nil
}
