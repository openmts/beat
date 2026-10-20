package releasemanifest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const maximumSpecBytes = 1 << 20

// DigestRef 指向一份证据：可以直接给出 64 位小写十六进制摘要，也可以给本地文件由本包计算。
type DigestRef struct {
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
}

// ArtifactSpec 描述一个发布产物；Size 与 SHA256 由 Build 从 Path 计算，避免手写出错。
type ArtifactSpec struct {
	ID         string    `json:"id"`
	OS         string    `json:"os"`
	Arch       string    `json:"arch"`
	Kind       string    `json:"kind"`
	Path       string    `json:"path"`
	Filename   string    `json:"filename,omitempty"`
	Entrypoint string    `json:"entrypoint"`
	Support    string    `json:"support"`
	SBOM       DigestRef `json:"sbom"`
	Provenance DigestRef `json:"provenance"`
}

// Spec 是发布描述文件；字段与 protocol.Manifest 一一对应，只把摘要换成可读的引用。
type Spec struct {
	Version             string         `json:"version"`
	Commit              string         `json:"commit"`
	Channel             string         `json:"channel"`
	BuiltAt             time.Time      `json:"built_at"`
	ExpiresAt           time.Time      `json:"expires_at"`
	MinimumServerSchema int            `json:"minimum_server_schema"`
	MinimumProtocol     int            `json:"minimum_protocol"`
	MaximumProtocol     int            `json:"maximum_protocol"`
	SigningKeyID        string         `json:"signing_key_id"`
	ReleaseNotes        DigestRef      `json:"release_notes"`
	Artifacts           []ArtifactSpec `json:"artifacts"`
}

// ParseSpec 严格解码发布描述：拒绝未知字段与尾随内容，长度受上限约束。
func ParseSpec(content []byte) (Spec, error) {
	var spec Spec
	if len(content) == 0 || len(content) > maximumSpecBytes {
		return spec, ErrSpec
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return spec, ErrSpec
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return spec, ErrSpec
	}
	return spec, nil
}

// Build 计算产物大小与摘要、解析证据引用，产出通过协议校验的 canonical manifest。
func Build(spec Spec, now time.Time) (protocol.Manifest, error) {
	manifest := protocol.Manifest{
		SchemaVersion: protocol.ManifestVersion, Version: spec.Version, Commit: spec.Commit,
		Channel: spec.Channel, BuiltAt: spec.BuiltAt.UTC(), ExpiresAt: spec.ExpiresAt.UTC(),
		MinimumServerSchema: spec.MinimumServerSchema, MinimumProtocol: spec.MinimumProtocol,
		MaximumProtocol: spec.MaximumProtocol, SigningKeyID: spec.SigningKeyID,
	}
	digest, err := resolveDigest(spec.ReleaseNotes)
	if err != nil {
		return protocol.Manifest{}, err
	}
	manifest.ReleaseNotesDigest = digest
	for _, artifact := range spec.Artifacts {
		compiled, err := compileArtifact(artifact)
		if err != nil {
			return protocol.Manifest{}, err
		}
		manifest.Artifacts = append(manifest.Artifacts, compiled)
	}
	if err := protocol.ValidateManifest(manifest, now); err != nil {
		return protocol.Manifest{}, ErrSpec
	}
	return manifest, nil
}

// compileArtifact 读取产物的真实大小与 SHA-256；摘要字段只来自文件或显式给出的摘要。
func compileArtifact(spec ArtifactSpec) (protocol.Artifact, error) {
	artifact := protocol.Artifact{ID: spec.ID, OS: spec.OS, Arch: spec.Arch, Kind: spec.Kind,
		Filename: spec.Filename, Entrypoint: spec.Entrypoint, Support: spec.Support}
	if artifact.Filename == "" {
		artifact.Filename = filepath.Base(spec.Path)
	}
	info, digest, err := statArtifact(spec.Path)
	if err != nil {
		return artifact, err
	}
	artifact.Size, artifact.SHA256 = info, digest
	if artifact.SBOMDigest, err = resolveDigest(spec.SBOM); err != nil {
		return artifact, err
	}
	if artifact.ProvenanceDigest, err = resolveDigest(spec.Provenance); err != nil {
		return artifact, err
	}
	return artifact, nil
}

// statArtifact 只接受普通文件，返回大小与内容摘要。
func statArtifact(path string) (int64, string, error) {
	if path == "" {
		return 0, "", ErrSpec
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, "", ErrSpec
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > protocol.MaximumArtifactBytes {
		return 0, "", ErrSpec
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return 0, "", ErrSpec
	}
	return info.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

// resolveDigest 优先使用显式摘要，否则计算引用文件的 SHA-256。
func resolveDigest(ref DigestRef) (string, error) {
	if ref.Digest != "" {
		if !protocol.ValidSHA256(ref.Digest) {
			return "", ErrSpec
		}
		return ref.Digest, nil
	}
	if ref.Path == "" {
		return "", ErrSpec
	}
	_, digest, err := statArtifact(ref.Path)
	return digest, err
}

// Sign 对 canonical payload 签名；调用方负责保证 payload 来自 Build 的输出。
func Sign(payload []byte, private ed25519.PrivateKey) (protocol.SignedManifest, error) {
	if len(private) != ed25519.PrivateKeySize {
		return protocol.SignedManifest{}, ErrSpec
	}
	return protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(private, payload)}, nil
}
