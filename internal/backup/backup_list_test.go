package backup

import (
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// listBackups 是测试辅助：业务代码只允许有界分页，测试需要整表视图。
func listBackups(t *testing.T, service *Service) ([]model.BackupRecord, error) {
	t.Helper()
	page, err := service.List(t.Context(), store.BackupQuery{Limit: store.BackupMaximumPage})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}
