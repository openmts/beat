package model

import "time"

type SSHKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	KeyType     string    `json:"key_type"`
	PublicKey   string    `json:"public_key"`
	PrivateKey  string    `json:"-"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}
