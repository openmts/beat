package beatcli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
)

// stdoutPath 表示把下载内容写入标准输出。
const stdoutPath = "-"

// createOutput 创建下载目标文件；除非显式要求，否则不覆盖已有文件。
func createOutput(path string, force bool) (*os.File, error) {
	mode := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		mode = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(path, mode, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("output file already exists: %s (pass --force to overwrite)", path)
		}
		return nil, fmt.Errorf("create output file: %w", err)
	}
	return file, nil
}

// closeDownload 关闭下载文件；下载失败时清理半成品，避免留下不完整归档。
func closeDownload(file *os.File, path string, downloadErr error) error {
	closeErr := file.Close()
	if downloadErr != nil {
		return removePartial(path, downloadErr)
	}
	if closeErr != nil {
		return removePartial(path, fmt.Errorf("close output file: %w", closeErr))
	}
	return nil
}

func removePartial(path string, cause error) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w (partial file remains: %v)", cause, err)
	}
	return cause
}

// downloadBackup 下载备份归档；"-" 写标准输出，其余路径默认拒绝覆盖已有文件。
func (s *session) downloadBackup(ctx context.Context, backupID, output string, force bool) error {
	path := backupsPath + "/" + url.PathEscape(backupID) + downloadPath
	if output == stdoutPath {
		written, err := s.client.Download(ctx, path, s.output)
		if err != nil {
			return err
		}
		return s.report(written, "standard output")
	}
	file, err := createOutput(output, force)
	if err != nil {
		return err
	}
	written, downloadErr := s.client.Download(ctx, path, file)
	if err := closeDownload(file, output, downloadErr); err != nil {
		return err
	}
	return s.report(written, output)
}

// report 把写入结果写到 stderr，避免污染标准输出上的 JSON 或归档内容。
func (s *session) report(written int64, target string) error {
	if s.summary == nil {
		return nil
	}
	_, err := fmt.Fprintf(s.summary, "wrote %d bytes to %s\n", written, target)
	return err
}
