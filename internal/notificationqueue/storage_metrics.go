package notificationqueue

import (
	"context"
	"path/filepath"

	"github.com/shirou/gopsutil/v4/disk"
)

func (service *Service) snapshotStorage(ctx context.Context, snapshot *QueueSnapshot) error {
	var pages, reusable, pageSize int64
	err := service.db.QueryRowContext(ctx, "SELECT page_count,freelist_count,page_size FROM pragma_page_count,pragma_freelist_count,pragma_page_size").Scan(&pages, &reusable, &pageSize)
	if err != nil {
		return ErrQueueUnavailable
	}
	snapshot.SQLiteBytes, snapshot.SQLiteReusableBytes = pages*pageSize, reusable*pageSize
	var path string
	if err := service.db.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		return ErrQueueUnavailable
	}
	if path == "" {
		return nil
	}
	usage, err := disk.UsageWithContext(ctx, filepath.Dir(path))
	if err == nil {
		snapshot.FilesystemFreeBytes = &usage.Free
	}
	return nil
}
