package agentenrollment

// RequestStatus 是入网请求的状态机状态：pending -> approved -> claimed，
// 且 pending/approved 都可以在认领前终止为 rejected 或 expired。
type RequestStatus string

const (
	RequestPending  RequestStatus = "pending"
	RequestApproved RequestStatus = "approved"
	RequestRejected RequestStatus = "rejected"
	RequestExpired  RequestStatus = "expired"
	RequestClaimed  RequestStatus = "claimed"
)

// NodeCredentialState 是节点凭据的显式状态；pending_claim 只为入网保留而存在。
type NodeCredentialState string

const (
	CredentialLegacy       NodeCredentialState = "legacy"
	CredentialPendingClaim NodeCredentialState = "pending_claim"
	CredentialActive       NodeCredentialState = "active"
	CredentialRevoked      NodeCredentialState = "revoked"
)

// CanApprove 表示请求仍等待审批。
func (status RequestStatus) CanApprove() bool { return status == RequestPending }

// CanReject 表示请求尚未认领，仍可被拒绝或判定过期。
func (status RequestStatus) CanReject() bool {
	return status == RequestPending || status == RequestApproved
}

// CanClaim 表示审批已完成、票据可以兑换节点凭据。
func (status RequestStatus) CanClaim() bool { return status == RequestApproved }

// Terminal 表示请求已进入终态，不再改变。
func (status RequestStatus) Terminal() bool {
	return status == RequestRejected || status == RequestExpired || status == RequestClaimed
}

// Valid 校验状态取值来自白名单，避免把历史或拼写错误当作有效状态。
func (status RequestStatus) Valid() bool {
	switch status {
	case RequestPending, RequestApproved, RequestRejected, RequestExpired, RequestClaimed:
		return true
	default:
		return false
	}
}

// CanAuthenticateAgent 只有 active 的节点凭据可以上报；legacy、pending_claim 与 revoked 都不行。
func (state NodeCredentialState) CanAuthenticateAgent() bool { return state == CredentialActive }

// Hidden 表示状态下的节点保留行不得出现在公共或 MTS 读路径里。
func (state NodeCredentialState) Hidden() bool { return state == CredentialPendingClaim }

// DeriveCredentialState 由既有节点列推导迁移后的凭据状态；只用于迁移回填。
func DeriveCredentialState(hasToken, revoked bool) NodeCredentialState {
	switch {
	case revoked:
		return CredentialRevoked
	case hasToken:
		return CredentialActive
	default:
		return CredentialLegacy
	}
}
