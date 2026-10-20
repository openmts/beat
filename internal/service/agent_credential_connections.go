package service

// credentialConnection 绑定握手时的凭据代际；原始token绝不进入注册表。
type credentialConnection interface{ CredentialBinding() string }

// DisconnectCredential 仅移除提交前对应代际；不会断开并发建立的新凭据连接。
func (h *AgentHub) DisconnectCredential(nodeID, binding string) {
	h.mu.Lock()
	conn := h.conns[nodeID]
	current, ok := conn.(credentialConnection)
	if !ok || current.CredentialBinding() != binding {
		h.mu.Unlock()
		return
	}
	delete(h.conns, nodeID)
	h.mu.Unlock()
	_ = conn.Close()
}
