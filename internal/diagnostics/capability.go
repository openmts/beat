package diagnostics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
)

// 线路类型直接复用共享协议包，保证服务端与 Agent 的规范字节完全一致。
type (
	CapabilityDocument = diagnosticprotocol.CapabilityDocument
	CapabilityMaxima   = diagnosticprotocol.CapabilityMaxima
	MTRCapability      = diagnosticprotocol.MTRCapability
	Iperf3Capability   = diagnosticprotocol.Iperf3Capability
	HopIdentity        = diagnosticprotocol.HopIdentity
	JobPayload         = diagnosticprotocol.JobPayload
	Lease              = diagnosticprotocol.Lease
)

const maximumCapabilityBytes = diagnosticprotocol.MaximumDocumentBytes

// Advertise 保存能力文档并返回其摘要；摘要参与后续每一次租约校验。
func (service *Service) Advertise(ctx context.Context, proof NodeProof, document CapabilityDocument) (string, error) {
	encoded, err := document.MarshalCanonical()
	if err != nil {
		return "", ErrInvalid
	}
	digest, err := document.Digest()
	if err != nil {
		return "", ErrInvalid
	}
	err = service.write(ctx, func(tx transaction) error {
		nodeID, err := tx.node(proof)
		if err != nil {
			return err
		}
		return tx.one(`INSERT INTO diagnostic_capabilities(node_id, digest, document_json, seen_at) VALUES(?,?,?,?)
			ON CONFLICT(node_id) DO UPDATE SET digest=excluded.digest, document_json=excluded.document_json,
			seen_at=excluded.seen_at`, nodeID, digest, string(encoded), tx.now)
	})
	return digest, err
}

// capability 读取节点能力并要求其摘要与新鲜度都匹配。
func (tx transaction) capability(nodeID string) (CapabilityDocument, string, error) {
	var encoded, digest string
	var seen sql.NullTime
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT digest, document_json, seen_at FROM diagnostic_capabilities WHERE node_id=?`,
		nodeID).Scan(&digest, &encoded, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return CapabilityDocument{}, "", ErrCapability
	}
	if err != nil {
		return CapabilityDocument{}, "", ErrUnavailable
	}
	if !seen.Valid || tx.now.Sub(seen.Time) > CapabilityLifetime || len(encoded) > maximumCapabilityBytes {
		return CapabilityDocument{}, "", ErrCapability
	}
	var document CapabilityDocument
	if json.Unmarshal([]byte(encoded), &document) != nil || document.Validate() != nil {
		return CapabilityDocument{}, "", ErrCapability
	}
	return document, digest, nil
}

// NodeCapability 是管理员视角的节点能力摘要；UI 展示节点名而不是 ID。
type NodeCapability struct {
	NodeID   string    `json:"node_id"`
	NodeName string    `json:"node_name"`
	Digest   string    `json:"digest"`
	SeenAt   time.Time `json:"seen_at"`
	State    string    `json:"state"`
	Kinds    []string  `json:"kinds"`
}

// Capabilities 列出所有已上报诊断能力的节点及其新鲜度。
func (service *Service) Capabilities(ctx context.Context, actor Actor) ([]NodeCapability, error) {
	capabilities := []NodeCapability{}
	err := service.read(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		rows, err := tx.tx.QueryContext(tx.ctx, `SELECT c.node_id, COALESCE(n.name,''), c.digest, c.document_json, c.seen_at
			FROM diagnostic_capabilities c LEFT JOIN nodes n ON n.id=c.node_id ORDER BY c.seen_at DESC LIMIT 500`)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var item NodeCapability
			var encoded string
			if err := rows.Scan(&item.NodeID, &item.NodeName, &item.Digest, &encoded, &item.SeenAt); err != nil {
				return ErrUnavailable
			}
			var document CapabilityDocument
			if json.Unmarshal([]byte(encoded), &document) != nil {
				item.State = "invalid"
				capabilities = append(capabilities, item)
				continue
			}
			item.Kinds = document.Kinds
			if document.State == "unavailable" || document.State == "partial" {
				item.State = document.State
			} else if tx.now.Sub(item.SeenAt) > CapabilityLifetime {
				item.State = "stale"
			} else {
				item.State = "ready"
			}
			capabilities = append(capabilities, item)
		}
		if err := rows.Err(); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return capabilities, err
}
