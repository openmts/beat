package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

type FileSpec struct {
	RootID       string `json:"root_id"`
	RelativePath string `json:"relative_path"`
	Size         int64  `json:"size"`
	Digest       string `json:"digest"`
	Mode         uint32 `json:"mode"`
	Overwrite    bool   `json:"overwrite"`
	ArtifactID   string `json:"artifact_id"`
}

func ValidateFile(file FileSpec) error {
	if !ValidID(file.RootID) || ValidateRelativePath(file.RelativePath) != nil || file.Size < 0 || file.Size > MaximumFileBytes || (file.Mode != 0o600 && file.Mode != 0o700) {
		return ErrInvalid
	}
	digest, err := hex.DecodeString(file.Digest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != file.Digest {
		return ErrInvalid
	}
	return nil
}

func AuthorizeFile(owner, local Policy, request FileRequest) error {
	if ValidateFile(request.File) != nil || (request.Direction != "deploy" && request.Direction != "collect") {
		return ErrForbidden
	}
	for _, policy := range []Policy{owner, local} {
		if !policy.Enabled || policy.Validate() != nil || !slices.Contains(policy.Kinds, request.Direction) {
			return ErrForbidden
		}
		root, ok := operationRoot(policy, request.File.RootID)
		if !ok || request.File.Size > root.MaximumBytes {
			return ErrForbidden
		}
		if request.Direction == "deploy" && (!root.Write || request.File.Overwrite && !root.Overwrite || request.File.Mode == 0o700 && !root.Executable) {
			return ErrForbidden
		}
		if request.Direction == "collect" && !root.Read {
			return ErrForbidden
		}
	}
	return nil
}

type FileRequest struct {
	Direction string
	File      FileSpec
}

func operationRoot(policy Policy, id string) (Root, bool) {
	for _, root := range policy.Roots {
		if root.ID == id {
			return root, true
		}
	}
	return Root{}, false
}

const FileChunkBytes = 4 * 1024 * 1024

type SourceInfo struct {
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type FileControlInput struct {
	Nonce    string      `json:"nonce"`
	Source   *SourceInfo `json:"source,omitempty"`
	Finalize bool        `json:"finalize,omitempty"`
}

type FileTransferReceipt struct {
	Acknowledged  bool  `json:"acknowledged"`
	MissingChunks []int `json:"missing_chunks"`
}
