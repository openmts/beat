package agentrollout

import "context"

type Status struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	Ready      bool   `json:"ready"`
	Reason     string `json:"reason"`
}

// Status在disabled时仍只读取真实owner权限；不要求v15表已登记。
func (s *Service) Status(ctx context.Context, actor Actor) (Status, error) {
	value := Status{Enabled: s.enabled, Configured: s.signaturePolicy != nil && (s.pinnedOnly || s.sigstore != nil) && len(s.keys) > 0, Reason: "disabled"}
	err := s.readOwner(ctx, actor, func(tx *transaction) error {
		if !value.Enabled {
			return nil
		}
		if !value.Configured {
			value.Reason = "unconfigured"
			return nil
		}
		value.Reason = "unavailable"
		if _, err := s.signaturePolicyRevision(tx); err != nil {
			return nil
		}
		if s.hasAvailableKey(tx) {
			value.Ready = true
			value.Reason = "ready"
		}
		return nil
	})
	return value, err
}

func (s *Service) hasAvailableKey(tx *transaction) bool {
	for id, key := range s.keys {
		if tx.at.Before(key.NotBefore) || !tx.at.Before(key.ExpiresAt) {
			continue
		}
		if _, err := s.trustedRevision(tx, id); err == nil {
			return true
		}
	}
	return false
}
