package agentrollout

import "context"

// Ready 仅检查启动时配置的信任和当前可用密钥，不以归档记录构造信任。
func (s *Service) Ready(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	database, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = database.Rollback() }()
	tx := &transaction{ctx: ctx, db: database, at: s.now(), username: "system"}
	if _, err := s.signaturePolicyRevision(tx); err != nil {
		return ErrUnavailable
	}
	if !s.hasAvailableKey(tx) {
		return ErrUnavailable
	}
	return nil
}
