package agentinstall

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"runtime"
)

// installerOS 返回上报给服务端的操作系统标识。
func installerOS() string { return runtime.GOOS }

// installerArchitecture 返回上报给服务端的 CPU 架构标识。
func installerArchitecture() string { return runtime.GOARCH }

// installNonce 生成一次安装的随机标识，用于区分同机重复安装。
func installNonce() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		host, hostErr := os.Hostname()
		if hostErr != nil {
			return "unknown"
		}
		return host
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}
