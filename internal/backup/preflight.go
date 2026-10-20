package backup

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// ArchivePreflight 是只读归档预检结果：归档结构、清单、校验和与解包内容都已校验。
// 解包内容留在调用方提供的私有临时目录内，调用方必须 Close 释放。
type ArchivePreflight struct {
	CreatedAt     time.Time
	DatabasePath  string
	BeatVersion   string
	FormatVersion int
	MetricRows    int64
	root          string
}

// InspectArchive 校验归档但不发布恢复日志、不接触实时数据库。
// stagingParent 必须是调用方拥有并负责清理的私有临时目录。
func InspectArchive(ctx context.Context, archivePath, stagingParent string) (*ArchivePreflight, error) {
	validated, err := validateArchive(ctx, archivePath, stagingParent)
	if err != nil {
		return nil, err
	}
	return &ArchivePreflight{
		CreatedAt: validated.manifest.CreatedAt, BeatVersion: validated.manifest.BeatVersion,
		FormatVersion: validated.manifest.FormatVersion, MetricRows: validated.manifest.MetricRows,
		DatabasePath: filepath.Join(validated.root, sqliteEntry), root: validated.root,
	}, nil
}

// Close 只删除本次预检生成的解包目录。
func (preflight *ArchivePreflight) Close() error {
	if preflight == nil || preflight.root == "" {
		return nil
	}
	root := preflight.root
	preflight.root = ""
	return os.RemoveAll(root)
}
