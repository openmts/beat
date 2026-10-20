package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
)

type TransferTarget struct {
	ID            string `json:"id"`
	NodeID        string `json:"node_id"`
	NodeName      string `json:"node_name"`
	State         string `json:"state"`
	ProgressBytes int64  `json:"progress_bytes"`
}

func (service *Service) ListTransfers(ctx context.Context, actor Actor) ([]TransferSummary, error) {
	var transfers []TransferSummary
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		transfers, err = tx.listTransfers()
		return err
	})
	return transfers, err
}

func (tx transaction) listTransfers() (items []TransferSummary, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT t.id,t.direction,t.state,t.byte_size,t.digest,COALESCE(t.task_id,''),COALESCE(a.expires_at,t.expires_at),
 COALESCE(t.direction='collect' AND a.state='ready' AND a.expires_at>? AND ((t.require_scan=0 AND COALESCE(json_extract(p.configuration_json,'$.require_scan'),0)=0) OR a.scan_status='clean'),0)
 FROM operation_transfers t LEFT JOIN operation_artifacts a ON a.id=t.artifact_id LEFT JOIN operation_policies p ON p.id=t.policy_id ORDER BY t.created_at DESC,t.id LIMIT 200`, tx.now.UnixMilli())
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	items = []TransferSummary{}
	for rows.Next() {
		var item TransferSummary
		if err := rows.Scan(&item.ID, &item.Direction, &item.State, &item.Size, &item.Digest, &item.TaskID, &item.ExpiresAt, &item.DownloadAvailable); err != nil {
			return nil, ErrUnavailable
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (service *Service) GetTransfer(ctx context.Context, actor Actor, id string) (TransferSummary, error) {
	var item TransferSummary
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		item, err = tx.transferSummary(id)
		return err
	})
	return item, err
}

func (tx transaction) transferSummary(id string) (TransferSummary, error) {
	var item TransferSummary
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT t.id,t.direction,t.state,t.byte_size,t.digest,COALESCE(t.task_id,''),COALESCE(a.expires_at,t.expires_at),
 COALESCE(t.direction='collect' AND a.state='ready' AND a.expires_at>? AND ((t.require_scan=0 AND COALESCE(json_extract(p.configuration_json,'$.require_scan'),0)=0) OR a.scan_status='clean'),0)
 FROM operation_transfers t LEFT JOIN operation_artifacts a ON a.id=t.artifact_id LEFT JOIN operation_policies p ON p.id=t.policy_id WHERE t.id=?`, tx.now.UnixMilli(), id).
		Scan(&item.ID, &item.Direction, &item.State, &item.Size, &item.Digest, &item.TaskID, &item.ExpiresAt, &item.DownloadAvailable)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, ErrUnavailable
	}
	item.MissingChunks, err = tx.missingChunks(id)
	if err != nil {
		return item, err
	}
	item.Targets, err = tx.transferTargetSummaries(id)
	return item, err
}

func (tx transaction) missingChunks(id string) (missing []int, resultErr error) {
	var artifactID sql.NullString
	var size int64
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT artifact_id,byte_size FROM operation_transfers WHERE id=?", id).Scan(&artifactID, &size); err != nil {
		return nil, ErrUnavailable
	}
	if size < 0 || size > MaximumFileBytes {
		return nil, ErrUnavailable
	}
	missing = []int{}
	if !artifactID.Valid {
		return missing, nil
	}
	rows, err := tx.tx.QueryContext(tx.ctx, "SELECT chunk_index FROM operation_artifact_chunks WHERE artifact_id=? ORDER BY chunk_index", artifactID.String)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	present := map[int]bool{}
	for rows.Next() {
		var index int
		if err := rows.Scan(&index); err != nil {
			return nil, ErrUnavailable
		}
		present[index] = true
	}
	for index := 0; int64(index)*SpoolChunkBytes < size; index++ {
		if !present[index] {
			missing = append(missing, index)
		}
	}
	return missing, rows.Err()
}

func (tx transaction) transferTargetSummaries(id string) (items []TransferTarget, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, "SELECT id,COALESCE(node_id,''),node_name,state,progress_bytes FROM operation_transfer_targets WHERE transfer_id=? ORDER BY node_name,id LIMIT 200", id)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	items = []TransferTarget{}
	for rows.Next() {
		var item TransferTarget
		if err := rows.Scan(&item.ID, &item.NodeID, &item.NodeName, &item.State, &item.ProgressBytes); err != nil {
			return nil, ErrUnavailable
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (service *Service) AgentFileReceipt(ctx context.Context, request AttemptRequest) (FileTransferReceipt, error) {
	var receipt FileTransferReceipt
	err := service.write(ctx, func(tx transaction) error {
		_, id, err := tx.attemptArtifact(request)
		if err != nil {
			return err
		}
		receipt.MissingChunks, err = tx.missingChunks(id)
		receipt.Acknowledged = err == nil
		return err
	})
	return receipt, err
}
