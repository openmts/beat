//go:build !linux && !darwin && !freebsd

package store

import "errors"

// 未实现 flock 的平台拒绝多进程共用 MTS 目录，保持失败关闭。
func acquireMTSOwnerLease(string) (*mtsOwnerLease, error) {
	return nil, errors.New("MTS store ownership is not supported on this platform")
}
