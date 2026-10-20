package store

import (
	"context"
	"time"
)

// CleanupEnrollmentStateAtStartup 在服务端启动或恢复后立刻终结过期挑战、票据与请求，
// 保证入网路由对外可用之前不会残留上一进程或备份里的过期状态。
func CleanupEnrollmentStateAtStartup(ctx context.Context, db *SQLiteStore) error {
	_, err := NewAgentEnrollmentStore(db.DB).CleanupEnrollmentState(ctx, time.Now().UTC())
	return err
}
