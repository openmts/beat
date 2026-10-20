package store

import (
	"context"
	"fmt"
)

// applyAcceleratorPublicVisibility 增加指标页 GPU 区域的总开关与节点级展示开关。
// 总开关存在站点设置里默认开启；节点没有记录时也按开启处理，升级后行为不变。
func applyAcceleratorPublicVisibility(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		`ALTER TABLE site_settings ADD COLUMN show_accelerators INTEGER NOT NULL DEFAULT 1 CHECK(show_accelerators IN (0, 1))`,
		`CREATE TABLE node_accelerator_visibility (
			node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
			visible INTEGER NOT NULL CHECK(visible IN (0, 1)),
			updated_at DATETIME NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply accelerator visibility schema: %w", err)
		}
	}
	return nil
}
