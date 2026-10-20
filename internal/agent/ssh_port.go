package agent

// DetectSSHPort 沿用配置加载时的 sshd Port / Include 探测规则。
func DetectSSHPort() (int, error) {
	return readSSHPort()
}
