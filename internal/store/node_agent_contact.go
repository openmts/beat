package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

var ErrAgentCredentialRejected = errors.New("agent credential is no longer authorized")

const agentContactTokenTouchInterval = 5 * time.Minute

type AgentContact struct {
	Identity          model.AgentIdentity
	Heartbeat         NodeHeartbeat
	PreserveInventory bool
	SourceIP          string
	Accelerators      *model.AcceleratorReport
	ReportID          string
	ReportCapturedAt  time.Time
}

func (s *NodeStore) WithGeoIPSecrets(secrets secretbox.ResourceService) *NodeStore {
	s.geoIPSecrets = secrets
	return s
}

func (s *NodeStore) AcceptAgentContact(ctx context.Context, contact AgentContact) (*model.Node, error) {
	if s.geoIPSecrets == nil || contact.SourceIP == "" || contact.Identity.Mode != model.AgentCredentialActive {
		return s.acceptAgentContact(ctx, contact, nil)
	}
	var node *model.Node
	err := s.geoIPSecrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		var err error
		node, err = s.acceptAgentContact(ctx, contact, access)
		return err
	})
	return node, err
}

func (s *NodeStore) acceptAgentContact(ctx context.Context, contact AgentContact, access secretbox.ResourceAccess) (*model.Node, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	node, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+
		" FROM nodes WHERE "+hiddenNodeFilter+" AND id=?", contact.Identity.NodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentCredentialRejected
	}
	if err != nil {
		return nil, err
	}
	if !matchesAgentCredential(node, contact.Identity) {
		return nil, ErrAgentCredentialRejected
	}
	if contact.PreserveInventory {
		contact.Heartbeat = storedNodeHeartbeat(node)
	}
	if err := stageAcceleratorContact(ctx, tx, contact); err != nil {
		return nil, err
	}
	contact.Heartbeat.Name = node.Name
	if err := updateHeartbeatWith(ctx, tx, heartbeatUpdate{node: &node, heartbeat: contact.Heartbeat}); err != nil {
		return nil, err
	}
	if contact.Identity.Mode == model.AgentCredentialActive {
		if err := touchAcceptedAgentToken(ctx, tx, node); err != nil {
			return nil, err
		}
	}
	if access != nil {
		if err := geoip.RecordObserved(ctx, tx, geoip.ObservedSource{NodeID: node.ID, Source: contact.SourceIP, Access: access, Now: node.LastSeen}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &node, nil
}

func storedNodeHeartbeat(node model.Node) NodeHeartbeat {
	return NodeHeartbeat{Name: node.Name, Host: node.Host, Port: node.Port, System: model.SystemInfo{
		CPUModel: node.CPUModel, OS: node.OS, Platform: node.Platform, OSVersion: node.OSVersion, Kernel: node.Kernel,
		Arch: node.Arch, Virtualization: node.Virtualization, AgentVersion: node.AgentVersion}}
}

func matchesAgentCredential(node model.Node, identity model.AgentIdentity) bool {
	if node.ID != identity.NodeID || node.AgentCredentialStatus() != identity.Mode {
		return false
	}
	if identity.Mode == model.AgentCredentialLegacy {
		return validReceiptDigest(identity.CredentialFingerprint)
	}
	if identity.Mode != model.AgentCredentialActive {
		return false
	}
	expected := model.AgentCredentialFingerprint(node.AgentTokenHash)
	return expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(identity.CredentialFingerprint)) == 1
}

func touchAcceptedAgentToken(ctx context.Context, tx *sql.Tx, node model.Node) error {
	_, err := tx.ExecContext(ctx, `UPDATE nodes SET agent_token_last_used_at=? WHERE id=?
		AND (agent_token_last_used_at IS NULL OR agent_token_last_used_at<=?)`, node.LastSeen, node.ID, node.LastSeen.Add(-agentContactTokenTouchInterval))
	return err
}
