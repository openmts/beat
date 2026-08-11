package sshclient

import (
	"fmt"

	"golang.org/x/crypto/ssh"
)

func parseSigner(privateKey string) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key: %w", err)
	}
	return signer, nil
}
