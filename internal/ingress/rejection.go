package ingress

import "fmt"

const maximumRetryAfter = 60

// Reason 是稳定的拒绝原因枚举，只作为分类标签使用，绝不携带客户端标识。
type Reason uint8

const (
	ReasonClassRate Reason = iota
	ReasonIdentityRate
	ReasonClassBusy
	ReasonIdentityBusy
	ReasonTableFull

	reasonCount
)

var reasonNames = [reasonCount]string{
	"class_rate",
	"identity_rate",
	"class_busy",
	"identity_busy",
	"table_full",
}

func (reason Reason) String() string {
	if reason >= reasonCount {
		return "unknown"
	}
	return reasonNames[reason]
}

// Rejection 是对外统一的拒绝结果：固定状态码、固定原因与有界 Retry-After。
type Rejection struct {
	Reason     Reason
	Status     int
	RetryAfter int
}

func (rejection *Rejection) Error() string {
	return fmt.Sprintf("ingress rejected: %s", rejection.Reason)
}

func newRejection(reason Reason, status, retryAfter int) *Rejection {
	return &Rejection{Reason: reason, Status: status, RetryAfter: clampRetryAfter(retryAfter)}
}

func clampRetryAfter(seconds int) int {
	return min(max(seconds, 1), maximumRetryAfter)
}

// 在途饱和是瞬时状态，固定等待一秒即可重试。
const (
	classBusyRetryAfter    = 1
	identityBusyRetryAfter = 1
	tableFullRetryAfter    = 5
)
