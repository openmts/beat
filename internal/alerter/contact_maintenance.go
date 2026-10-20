package alerter

import "context"

// MaintainContacts 单独维护节点contact状态，不读取或求值告警规则。
func (a *Alerter) MaintainContacts(ctx context.Context) error { return a.markStaleAgentNodes(ctx) }
