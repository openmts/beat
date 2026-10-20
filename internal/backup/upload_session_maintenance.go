package backup

import (
	"context"

	"github.com/beat/backend/internal/model"
)

// uploadMaintenanceBatch 限制每次维护运行处理的会话数。
const uploadMaintenanceBatch = 64

// MaintainUploadSessions 过期陈旧会话、清理私有暂存目录并裁剪会话历史。
func (service *Service) MaintainUploadSessions(ctx context.Context) error {
	if service.uploads == nil {
		return nil
	}
	if _, err := service.uploads.Expire(ctx, service.now()); err != nil {
		return err
	}
	sessions, err := service.uploads.PendingStagingCleanup(ctx, uploadMaintenanceBatch)
	if err != nil {
		return err
	}
	for index := range sessions {
		service.discardStaging(&sessions[index])
	}
	_, err = service.uploads.Prune(ctx, model.MaximumBackupUploadsPerOwner, uploadMaintenanceBatch)
	return err
}
