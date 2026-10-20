package remoteoperations

import "github.com/beat/backend/internal/remoteoperations/protocol"

type (
	SourceInfo          = protocol.SourceInfo
	FileControlInput    = protocol.FileControlInput
	FileTransferReceipt = protocol.FileTransferReceipt
	FileSpec            = protocol.FileSpec
	FileRequest         = protocol.FileRequest

	Root                 = protocol.Root
	Policy               = protocol.Policy
	Limits               = protocol.Limits
	TaskInput            = protocol.TaskInput
	Lease                = protocol.Lease
	LeaseRequest         = protocol.LeaseRequest
	NodeProof            = protocol.NodeProof
	Attempt              = protocol.Attempt
	Proof                = protocol.Proof
	Result               = protocol.Result
	AttemptRequest       = protocol.AttemptRequest
	Completion           = protocol.Completion
	AgentLeaseInput      = protocol.AgentLeaseInput
	AgentAttemptInput    = protocol.AgentAttemptInput
	AgentCompletionInput = protocol.AgentCompletionInput
	StartAuthorization   = protocol.StartAuthorization
	HeartbeatResponse    = protocol.HeartbeatResponse
	CompletionReceipt    = protocol.CompletionReceipt
)

const (
	MaximumCommandBytes   = protocol.MaximumCommandBytes
	MaximumOutputBytes    = protocol.MaximumOutputBytes
	MaximumTimeoutSeconds = protocol.MaximumTimeoutSeconds
	MaximumTargets        = protocol.MaximumTargets
	MaximumConcurrency    = protocol.MaximumConcurrency
	MaximumFileBytes      = protocol.MaximumFileBytes
	MaximumPathBytes      = protocol.MaximumPathBytes
	MaximumPathComponents = protocol.MaximumPathComponents
	LeaseDuration         = protocol.LeaseDuration
	HeartbeatGrace        = protocol.HeartbeatGrace
)

var (
	AuthorizeShell       = protocol.AuthorizeShell
	ErrBusy              = protocol.ErrBusy
	ValidateFile         = protocol.ValidateFile
	AuthorizeFile        = protocol.AuthorizeFile
	ErrInvalid           = protocol.ErrInvalid
	ErrForbidden         = protocol.ErrForbidden
	ErrConflict          = protocol.ErrConflict
	ErrNotFound          = protocol.ErrNotFound
	ErrUnavailable       = protocol.ErrUnavailable
	ErrCorrupt           = errArtifactCorrupt
	Authorize            = protocol.Authorize
	ValidateCommand      = protocol.ValidateCommand
	ValidateRelativePath = protocol.ValidateRelativePath
	validID              = protocol.ValidID
)
