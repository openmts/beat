package releasesignature

import (
	"context"
	"sync"
)

// boundedOutput 不保留外部工具的诊断文本，防止证书/路径/上传内容进入管理响应。
type boundedOutput struct {
	mu           sync.Mutex
	remaining    int
	limitReached bool
	cancel       context.CancelFunc
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if len(data) > output.remaining {
		output.limitReached = true
		output.cancel()
		return 0, ErrOutputLimit
	}
	output.remaining -= len(data)
	return len(data), nil
}

func (output *boundedOutput) exceeded() bool {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.limitReached
}
