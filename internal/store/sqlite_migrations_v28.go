package store

import "context"

// applyAgentTaskOutput 为任务结果增加有界诊断字段：命令输出前缀与失败原因。
// 两者都是展示与排障证据，任何写入都必须先按 model 上限裁剪。
func applyAgentTaskOutput(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`ALTER TABLE agent_tasks ADD COLUMN output TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_tasks ADD COLUMN failure TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
