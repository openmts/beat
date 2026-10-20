package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"

	"github.com/beat/backend/internal/model"
)

func nodeUpdateEvidence(before, after model.Node) metadataEvidence {
	fields := []string{}
	checks := []struct {
		name    string
		changed bool
	}{
		{"alias", before.Alias != after.Alias}, {"group_id", before.GroupID != after.GroupID},
		{"ssh_public_key", before.SSHPublicKey != after.SSHPublicKey}, {"traffic_limit", before.TrafficLimit != after.TrafficLimit},
		{"traffic_limit_type", before.TrafficLimitType != after.TrafficLimitType}, {"traffic_reset_day", before.TrafficResetDay != after.TrafficResetDay},
		{"sort_order", before.SortOrder != after.SortOrder}, {"tags", !slices.Equal(before.Tags, after.Tags)},
		{"is_public", before.IsPublic != after.IsPublic}, {"public_remark", before.PublicRemark != after.PublicRemark},
		{"private_remark", before.PrivateRemark != after.PrivateRemark},
	}
	for _, check := range checks {
		if check.changed {
			fields = append(fields, check.name)
		}
	}
	return metadataEvidence{Action: "node.update", Type: "node", ID: after.ID, Name: after.Name, Fields: fields}
}

// WithCredentialInvalidator 在服务启动时连接注册表；仅在凭据事务提交后通知。
func (s *NodeStore) WithCredentialInvalidator(invalidate func(string, string)) *NodeStore {
	s.credentialInvalidator = invalidate
	return s
}

func nodeCredentialBinding(node model.Node) string {
	if node.AgentCredentialStatus() == model.AgentCredentialLegacy {
		return model.AgentCredentialLegacy
	}
	return node.AgentCredentialStatus() + ":" + model.AgentCredentialFingerprint(node.AgentTokenHash)
}

// VerifyAgentIdentity 为长连接发送前和周期复验提供当前持久凭据状态。
func (s *NodeStore) VerifyAgentIdentity(ctx context.Context, identity model.AgentIdentity) error {
	node, err := s.GetNode(ctx, identity.NodeID)
	if err != nil {
		return err
	}
	if node == nil || !matchesAgentCredential(*node, identity) {
		return errors.New("agent identity is no longer valid")
	}
	return nil
}

func verifyNodeSnapshot(ctx context.Context, tx *sql.Tx, expected model.Node) error {
	actual, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+" FROM nodes WHERE id=?", expected.ID))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("node response snapshot changed before commit")
	}
	return nil
}
