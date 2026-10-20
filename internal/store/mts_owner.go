package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrMTSStoreBusy 表示同一 MTS 目录已被另一个进程独占。调用方应把它当成
// 启动期配置错误而不是可重试的瞬时故障。
var ErrMTSStoreBusy = errors.New("MTS store is already open")

const (
	mtsOwnerRecordLimit = 256
	mtsOwnerLockPrefix  = ".beat-mts-owner-"
)

type mtsOwnerRecord struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

// mtsOwnerLease 持有 MTS 目录的跨进程独占凭证。锁文件从不删除，避免
// 释放与重建之间出现另一个进程可抢占的窗口。
type mtsOwnerLease struct {
	file *os.File
	path string
	once sync.Once
	err  error
}

func (lease *mtsOwnerLease) Close() error {
	if lease == nil {
		return nil
	}
	lease.once.Do(func() {
		if lease.file == nil {
			return
		}
		lease.err = lease.file.Close()
	})
	return lease.err
}

// Verify 确认锁文件仍是被本进程打开的那一个。外部进程删除并重建锁文件后，
// 本进程持有的 flock 会退化成已删除 inode 上的孤儿锁，必须尽快失败关闭。
func (lease *mtsOwnerLease) Verify() error {
	if lease == nil || lease.file == nil {
		return errors.New("MTS store owner lease is not held")
	}
	return sameMTSOwnerFile(lease.path, lease.file)
}

// MTSOwnerLockPath 返回指定 MTS 目录的 owner 锁路径。锁落在目录的父目录，
// 上游引擎只扫描数据目录本身，不会把锁文件当成孤儿分片清理。
func MTSOwnerLockPath(path string) (string, error) {
	canonical, err := canonicalMTSDir(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(canonical))
	return filepath.Join(filepath.Dir(canonical), fmt.Sprintf("%s%x.lock", mtsOwnerLockPrefix, digest)), nil
}

func canonicalMTSDir(path string) (string, error) {
	if path == "" {
		return "", errors.New("MTS store directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve MTS store directory: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return canonical, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve MTS store directory: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve MTS store parent: %w", err)
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func sameMTSOwnerFile(path string, file *os.File) error {
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat MTS owner lease: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat MTS owner file: %w", err)
	}
	if !os.SameFile(opened, current) {
		return errors.New("MTS owner file changed while held")
	}
	return nil
}

func writeMTSOwnerRecord(file *os.File) error {
	record, err := json.Marshal(mtsOwnerRecord{PID: os.Getpid(), StartedAt: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return fmt.Errorf("encode MTS owner record: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate MTS owner file: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind MTS owner file: %w", err)
	}
	if _, err := file.Write(record); err != nil {
		return fmt.Errorf("write MTS owner record: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync MTS owner record: %w", err)
	}
	return nil
}

func mtsOwnerBusyError(path string) error {
	if pid, ok := readMTSOwnerPID(path); ok {
		return fmt.Errorf("%w (held by pid %d)", ErrMTSStoreBusy, pid)
	}
	return ErrMTSStoreBusy
}

func readMTSOwnerPID(path string) (int, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > mtsOwnerRecordLimit {
		return 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, mtsOwnerRecordLimit))
	if err != nil {
		return 0, false
	}
	var record mtsOwnerRecord
	if err := json.Unmarshal(data, &record); err != nil || record.PID <= 0 {
		return 0, false
	}
	return record.PID, true
}
