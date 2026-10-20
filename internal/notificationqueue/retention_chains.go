package notificationqueue

import (
	"context"
	"database/sql"
	"errors"
)

const maximumPrunedDeliveries = 128

type retentionCutoffs struct{ success, dead int64 }

func pruneDeliveryChains(ctx context.Context, tx *sql.Tx, cutoffs retentionCutoffs) error {
	// 只在本次事务内暂缓外键检查；移除过期祖先时把保留的直接后继提升为链首。
	// 当前 leaf 始终保留，SourceReceipt 不会回退到较早的 dead 状态。
	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return ErrQueueUnavailable
	}
	remaining := maximumPrunedDeliveries
	for remaining > 0 {
		roots, err := expiredDeliveryRoots(ctx, tx, rootRetention{cutoffs: cutoffs, limit: remaining})
		if err != nil {
			return err
		}
		if len(roots) == 0 {
			return nil
		}
		for _, root := range roots {
			if err := pruneDeliveryRoot(ctx, tx, root); err != nil {
				return err
			}
			remaining--
		}
	}
	return nil
}

type rootRetention struct {
	cutoffs retentionCutoffs
	limit   int
}

type deliveryRoot struct{ id, child string }

func expiredDeliveryRoots(ctx context.Context, tx *sql.Tx, input rootRetention) (roots []deliveryRoot, resultErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.id,COALESCE(child.id,'') FROM notification_deliveries d
        LEFT JOIN notification_deliveries child ON child.retry_of=d.id WHERE d.retry_of IS NULL
        AND ((d.state IN ('succeeded','cancelled') AND d.completed_at<?) OR (d.state='dead' AND d.completed_at<?))
        ORDER BY d.completed_at,d.id LIMIT ?`, input.cutoffs.success, input.cutoffs.dead, input.limit)
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var root deliveryRoot
		if err := rows.Scan(&root.id, &root.child); err != nil {
			return nil, ErrQueueUnavailable
		}
		roots = append(roots, root)
	}
	return roots, rows.Err()
}

func pruneDeliveryRoot(ctx context.Context, tx *sql.Tx, root deliveryRoot) error {
	result, err := tx.ExecContext(ctx, "DELETE FROM notification_deliveries WHERE id=? AND retry_of IS NULL", root.id)
	if err := exactlyOne(result, err); err != nil {
		return ErrQueueUnavailable
	}
	if root.child == "" {
		return nil
	}
	result, err = tx.ExecContext(ctx, "UPDATE notification_deliveries SET retry_of=NULL WHERE id=? AND retry_of=?", root.child, root.id)
	if err := exactlyOne(result, err); err != nil {
		return ErrQueueUnavailable
	}
	return nil
}
