//go:build !linux && !darwin && !freebsd

package agentrollout

import "os"

// 未实现安全文件打开的平台不提供自托管发布资产。
func openArtifactFile(string) (*os.File, error) { return nil, ErrUnavailable }

func trustedReleaseOwner(os.FileInfo) bool { return false }

func ownedReleaseFile(os.FileInfo) bool { return false }
