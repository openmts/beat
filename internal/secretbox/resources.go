package secretbox

import "context"

// ResourceAccess 只在 WithWrite 回调中有效；调用方须在回调返回前提交密文。
type ResourceAccess interface {
	Seal([]byte, ResourceAAD) ([]byte, error)
	Open([]byte, ResourceAAD) ([]byte, error)
}

// ResourceService 把选键、加密与持久化放在一个写入窗口，与轮换/退役互斥。
type ResourceService interface {
	WithWrite(context.Context, func(ResourceAccess) error) error
	OpenResource(context.Context, []byte, ResourceAAD) ([]byte, error)
}
