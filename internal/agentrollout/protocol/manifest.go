package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const MaximumManifestBytes = 1 << 20

const MaximumIdentifierBytes = 128

const MaximumArtifactBytes int64 = 512 << 20

const maximumArtifacts = 32

var safeIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]{0,127}$`)

var commitDigest = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)

var lowerDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// CanonicalManifest 使用本协议定义的固定字段顺序；仅签署返回的精确UTF-8字节。
func CanonicalManifest(manifest Manifest) ([]byte, error) { return json.Marshal(manifest) }

func DecodeManifest(payload []byte) (Manifest, error) {
	var manifest Manifest
	if len(payload) == 0 || len(payload) > MaximumManifestBytes {
		return manifest, ErrManifest
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, ErrManifest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return manifest, ErrManifest
	}
	canonical, err := CanonicalManifest(manifest)
	if err != nil || !bytes.Equal(canonical, payload) {
		return manifest, ErrManifest
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest, now time.Time) error {
	if !validManifestIdentity(manifest) || !validManifestDates(manifest, now) {
		return ErrManifest
	}
	if manifest.MinimumServerSchema < 1 || manifest.MinimumProtocol < 1 || manifest.MaximumProtocol < manifest.MinimumProtocol {
		return ErrManifest
	}
	return validateManifestArtifacts(manifest.Artifacts)
}

func validManifestIdentity(manifest Manifest) bool {
	_, valid := parseVersion(manifest.Version)
	return valid && manifest.SchemaVersion == ManifestVersion && safeIdentifier.MatchString(manifest.SigningKeyID) && commitDigest.MatchString(manifest.Commit) && lowerDigest.MatchString(manifest.ReleaseNotesDigest) && (manifest.Channel == "stable" || manifest.Channel == "preview")
}

func validManifestDates(manifest Manifest, now time.Time) bool {
	return !manifest.BuiltAt.IsZero() && !manifest.BuiltAt.After(now.Add(time.Minute)) && manifest.ExpiresAt.After(now) && manifest.ExpiresAt.After(manifest.BuiltAt) && !manifest.Revoked
}

func validateManifestArtifacts(artifacts []Artifact) error {
	if len(artifacts) == 0 || len(artifacts) > maximumArtifacts {
		return ErrManifest
	}
	ids, platforms := map[string]bool{}, map[string]bool{}
	for _, artifact := range artifacts {
		platform := artifact.OS + "/" + artifact.Arch + "/" + artifact.Kind
		if err := ValidateArtifact(artifact); err != nil {
			return err
		}
		if ids[artifact.ID] || platforms[platform] {
			return ErrManifest
		}
		ids[artifact.ID], platforms[platform] = true, true
	}
	return nil
}

func ValidateArtifact(a Artifact) error {
	if !validArtifactPayload(a) || !validArtifactPlatform(a) || !SafeFilename(a.Filename) || !SafeFilename(a.Entrypoint) {
		return ErrManifest
	}
	return nil
}

func validArtifactPayload(a Artifact) bool {
	return safeIdentifier.MatchString(a.ID) && a.Size > 0 && a.Size <= MaximumArtifactBytes && lowerDigest.MatchString(a.SHA256) && lowerDigest.MatchString(a.SBOMDigest) && lowerDigest.MatchString(a.ProvenanceDigest)
}

func validArtifactPlatform(a Artifact) bool {
	return slices.Contains([]string{"linux", "windows", "darwin", "freebsd"}, a.OS) && slices.Contains([]string{"amd64", "arm64", "386", "arm", "loong64"}, a.Arch) && slices.Contains([]string{"binary", "archive", "external_manager"}, a.Kind) && slices.Contains([]string{"supported", "preview", "build-only", "unsupported"}, a.Support)
}

func SafeFilename(value string) bool {
	return value != "" && len(value) <= MaximumIdentifierBytes && filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n") && value != "." && value != ".."
}

func ManifestDigest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func VerifyTransition(current ed25519.PublicKey, next *KeyTransition, now time.Time) error {
	if next == nil {
		return nil
	}
	if !safeIdentifier.MatchString(next.ID) || len(next.PublicKey) != ed25519.PublicKeySize || !next.ExpiresAt.After(next.NotBefore) || !next.ExpiresAt.After(now) {
		return ErrManifest
	}
	payload, err := TransitionSigningBytes(*next)
	if err != nil || len(current) != ed25519.PublicKeySize || !ed25519.Verify(current, payload, next.Signature) {
		return ErrSignature
	}
	return nil
}

func ValidSHA256(value string) bool { return lowerDigest.MatchString(value) }

func ValidSigningKeyID(value string) bool { return safeIdentifier.MatchString(value) }

func TransitionSigningBytes(next KeyTransition) ([]byte, error) {
	next.Signature = nil
	return json.Marshal(next)
}
