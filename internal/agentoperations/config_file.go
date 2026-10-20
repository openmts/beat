package agentoperations

// CheckPrivateConfigFile 共用平台权限校验；Windows 使用服务账户 DACL 而非 Unix mode。
func CheckPrivateConfigFile(path string) error {
	if err := privateFile(path); err != nil {
		return err
	}
	return checkSensitiveFile(path)
}
