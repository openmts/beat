package agentrollout

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"
)

// pinnedSignatureMode 是只锚定启动时固定 Ed25519 公钥的信任模式：它没有 Sigstore 策略，
// 信任根就是这份公钥清单本身，因此把清单的规范化编码当作策略文档与 root 摘要落库，
// 让 `agent_releases.signature_policy_revision` 在两种模式下含义一致。
const pinnedSignatureMode = "pinned_ed25519"

type pinnedPolicyDocument struct {
	Version int               `json:"version"`
	Mode    string            `json:"mode"`
	Keys    []pinnedPolicyKey `json:"keys"`
}

type pinnedPolicyKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	NotBefore string `json:"not_before"`
	ExpiresAt string `json:"expires_at"`
}

// snapshotPinnedPolicy 按 key id 排序生成确定性文档；同一份公钥清单永远得到同一 revision。
func snapshotPinnedPolicy(keys map[string]TrustedKey) (*signaturePolicySnapshot, error) {
	if len(keys) == 0 {
		return nil, ErrInvalid
	}
	entries := make([]pinnedPolicyKey, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, pinnedPolicyKey{
			ID:        key.ID,
			PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey),
			NotBefore: key.NotBefore.UTC().Format(time.RFC3339Nano),
			ExpiresAt: key.ExpiresAt.UTC().Format(time.RFC3339Nano),
		})
	}
	slices.SortFunc(entries, func(left, right pinnedPolicyKey) int {
		switch {
		case left.ID < right.ID:
			return -1
		case left.ID > right.ID:
			return 1
		default:
			return 0
		}
	})
	root, err := json.Marshal(entries)
	if err != nil || len(root) > maximumPublicRootBytes {
		return nil, ErrSignature
	}
	document, err := json.Marshal(pinnedPolicyDocument{Version: 1, Mode: pinnedSignatureMode, Keys: entries})
	if err != nil || len(document) > maximumPolicyDocumentBytes {
		return nil, ErrSignature
	}
	rootDigest := sha256.Sum256(root)
	revision := sha256.Sum256([]byte(pinnedSignatureMode + "\x00" + hex.EncodeToString(rootDigest[:])))
	return &signaturePolicySnapshot{revision: hex.EncodeToString(revision[:]), rootDigest: hex.EncodeToString(rootDigest[:]), document: document}, nil
}
