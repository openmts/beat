//go:build !unix

package releasesignature

import "os"

// 本适配器的本地文件信任检查仅验收Unix；其他平台不能跳过所有者检查。
func trustedOwner(_ os.FileInfo) bool { return false }
