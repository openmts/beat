package releasesignature

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"slices"
	"strings"
)

const maximumVerifierBytes = 512 << 20

// PolicyMetadata 仅包含可备份的公开信任内容，恢复不得用它替代带外安装的锚。
type PolicyMetadata struct {
	Version             int    `json:"version"`
	Root                []byte `json:"root"`
	RootSHA256          string `json:"root_sha256"`
	VerifierSHA256      string `json:"verifier_sha256"`
	CertificateIdentity string `json:"certificate_identity"`
	ReleaseWorkflow     string `json:"release_workflow"`
	OIDCIssuer          string `json:"oidc_issuer"`
	Revision            string `json:"revision"`
}

func (v *Verifier) Policy() PolicyMetadata {
	if v == nil || v.slots == nil {
		return PolicyMetadata{}
	}
	rootDigest := sha256.Sum256(v.config.TrustedRoot)
	policy := PolicyMetadata{Version: 1, Root: slices.Clone(v.config.TrustedRoot), RootSHA256: hex.EncodeToString(rootDigest[:]), VerifierSHA256: v.executableDigest,
		CertificateIdentity: v.config.CertificateIdentity, ReleaseWorkflow: v.config.ReleaseWorkflow, OIDCIssuer: v.config.OIDCIssuer}
	// 所有策略字符串已拒绝NUL；固定顺序和分隔符避免拼接歧义。
	digest := sha256.Sum256([]byte(strings.Join([]string{"1", policy.RootSHA256, policy.VerifierSHA256, policy.CertificateIdentity, policy.ReleaseWorkflow, policy.OIDCIssuer}, "\x00")))
	policy.Revision = hex.EncodeToString(digest[:])
	return policy
}

func verifierDigest(path string, expected os.FileInfo) (value string, resultErr error) {
	if expected.Size() <= 0 || expected.Size() > maximumVerifierBytes {
		return "", ErrConfiguration
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrConfiguration
	}
	defer func() {
		if err := file.Close(); err != nil {
			resultErr = ErrConfiguration
		}
	}()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(expected, opened) {
		return "", ErrConfiguration
	}
	value, err = readVerifierHash(file, expected.Size())
	if err != nil {
		return "", err
	}
	current, err := os.Lstat(path)
	if err != nil || !sameExecutableSnapshot(expected, current) {
		return "", ErrConfiguration
	}
	return value, nil
}

func readVerifierHash(file *os.File, expectedSize int64) (string, error) {
	digest := sha256.New()
	count, err := io.Copy(digest, io.LimitReader(file, maximumVerifierBytes+1))
	if err != nil || count != expectedSize {
		return "", ErrConfiguration
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func sameExecutableSnapshot(expected, current os.FileInfo) bool {
	return current.Mode().IsRegular() && os.SameFile(current, expected) && current.Size() == expected.Size() && current.ModTime().Equal(expected.ModTime())
}
