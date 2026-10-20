package protocol

// AgentLeaseInput 只公布能力根 ID；本机绝对路径和运行身份不进入网络协议。
type AgentLeaseInput struct {
	CapabilityDigest string `json:"capability_digest"`
	Capabilities     Policy `json:"capabilities"`
}

type AgentAttemptInput struct {
	Nonce string `json:"nonce"`
}

type AgentCompletionInput struct {
	Nonce  string `json:"nonce"`
	Result Result `json:"result"`
}

type StartAuthorization struct {
	Authorized bool `json:"authorized"`
}

type HeartbeatResponse struct {
	CancelRequested bool `json:"cancel_requested"`
}

type CompletionReceipt struct {
	Acknowledged bool `json:"acknowledged"`
}

// AgentResponse 与现有 API JSON envelope 一致；无 lease 时 Data 为 nil。
type AgentResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}
