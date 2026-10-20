package protocol

type TaskInput struct {
	File           *FileSpec `json:"file,omitempty"`
	ShellFamily    string    `json:"shell_family"`
	SnippetID      string    `json:"snippet_id,omitempty"`
	SnippetVersion int       `json:"snippet_version,omitempty"`
	DedupeKey      string    `json:"dedupe_key"`
	PolicyID       string    `json:"policy_id"`
	Command        []byte    `json:"command"`
	NodeIDs        []string  `json:"node_ids"`
	GroupIDs       []string  `json:"group_ids"`
	Limits         Limits    `json:"limits"`
}

type LeaseRequest struct {
	Proof            NodeProof
	CapabilityDigest string
}

type Lease struct {
	AttemptID string    `json:"attempt_id"`
	Nonce     string    `json:"nonce"`
	ExpiresAt int64     `json:"expires_at"`
	Payload   TaskInput `json:"payload"`
}

type NodeProof struct {
	Token string `json:"-"`
}

type AttemptRequest struct {
	Node  NodeProof
	ID    string
	Nonce string
}

type Completion struct {
	Attempt AttemptRequest
	Result  Result
}
