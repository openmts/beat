// Package migrationstatus 维护启动期迁移状态标记：一个有界、脱敏、原子发布的
// 运维产物，供没有可用浏览器时的本地诊断使用。它不是应用数据，也不进 SQLite。
package migrationstatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileName 是数据目录下的固定标记文件名。
const FileName = "migration.status.json"

const (
	maximumMarkerBytes = 4096
	maximumNameBytes   = 64
	maximumCodeBytes   = 32
	maximumBuildBytes  = 64
	maximumVersion     = 1000000
)

// 固定状态枚举：读写双方只接受这些值。
const (
	StateIdle      = "idle"
	StateChecking  = "checking"
	StateApplying  = "applying"
	StateFailed    = "failed"
	StateCompleted = "completed"
)

// ErrMarkerAbsent 表示本次部署还没有写入迁移状态标记。
var ErrMarkerAbsent = errors.New("migration status marker is absent")

// Marker 是标记的完整内容；所有字段都是运维状态，绝不含 SQL、路径或应用数据。
type Marker struct {
	State              string    `json:"state"`
	Phase              string    `json:"phase"`
	FailureCode        string    `json:"failure_code"`
	MigrationName      string    `json:"migration_name"`
	ApplicationVersion string    `json:"application_version"`
	BuildID            string    `json:"build_id"`
	SourceSchema       int       `json:"source_schema"`
	TargetSchema       int       `json:"target_schema"`
	CurrentSchema      int       `json:"current_schema"`
	CurrentMigration   int       `json:"current_migration"`
	RollbackRequired   bool      `json:"rollback_required"`
	StartedAt          time.Time `json:"started_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CompletedAt        time.Time `json:"completed_at,omitempty"`
}

// Writer 把标记原子发布到私有数据目录；时间源可被测试替换。
type Writer struct {
	directory, version, build string
	now                       func() time.Time
	started                   time.Time
	source, target, applied   int
	inProgress                int
	name                      string
}

// NewWriter 创建写入器；目录必须是已存在的私有数据目录。
func NewWriter(directory, version, build string) *Writer {
	now := time.Now().UTC()
	return &Writer{directory: directory, version: version, build: build, now: func() time.Time { return time.Now().UTC() }, started: now}
}

// SetClock 覆盖时间源，供确定性测试使用。
func (writer *Writer) SetClock(now func() time.Time) { writer.now = now }

// Reset 记录一次新的部署尝试，把标记置回 idle。
func (writer *Writer) Reset() error {
	return writeMarker(writer.directory, writer.marker(StateIdle, ""))
}

// Started 记录来源与目标 schema，并进入 checking。
func (writer *Writer) Started(source, target int) error {
	writer.source, writer.target, writer.applied = source, target, source
	return writeMarker(writer.directory, writer.marker(StateChecking, "check"))
}

// Applied 记录正在应用的迁移，并推进已完成的 schema 版本。
func (writer *Writer) Applied(number int, name string) error {
	if number > writer.applied {
		writer.applied = number
	}
	writer.inProgress, writer.name = number, truncate(name, maximumNameBytes)
	return writeMarker(writer.directory, writer.marker(StateApplying, "apply"))
}

// Failed 记录固定失败代码与是否需要外部回滚。
func (writer *Writer) Failed(code string, rollbackRequired bool) error {
	marker := writer.marker(StateFailed, "failed")
	marker.FailureCode = truncate(code, maximumCodeBytes)
	marker.RollbackRequired = rollbackRequired
	return writeMarker(writer.directory, marker)
}

// Completed 记录迁移成功完成；完成标记保留到下一次部署尝试。
func (writer *Writer) Completed() error {
	writer.applied, writer.inProgress, writer.name = writer.target, 0, ""
	marker := writer.marker(StateCompleted, "ready")
	marker.CompletedAt = marker.UpdatedAt
	return writeMarker(writer.directory, marker)
}

func (writer *Writer) marker(state, phase string) Marker {
	return Marker{State: state, Phase: phase, MigrationName: writer.name, ApplicationVersion: writer.version,
		BuildID: writer.build, SourceSchema: writer.source, TargetSchema: writer.target,
		CurrentSchema: writer.applied, CurrentMigration: writer.inProgress,
		StartedAt: writer.started, UpdatedAt: writer.now()}
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func writeMarker(directory string, marker Marker) error {
	if err := marker.validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if len(encoded) > maximumMarkerBytes {
		return errors.New("migration status marker exceeds its bound")
	}
	return publish(directory, encoded)
}

func publish(directory string, encoded []byte) (resultErr error) {
	file, err := os.CreateTemp(directory, ".migration-status-*.tmp")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temp)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, filepath.Join(directory, FileName)); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) (resultErr error) {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.Close()) }()
	return handle.Sync()
}

// Read 读取并校验标记；缺失时返回 ErrMarkerAbsent。
func Read(directory string) (Marker, error) {
	path := filepath.Join(directory, FileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Marker{}, ErrMarkerAbsent
	}
	if err != nil {
		return Marker{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximumMarkerBytes {
		return Marker{}, errors.New("migration status marker is not a bounded regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Marker{}, err
	}
	var marker Marker
	if err := json.Unmarshal(content, &marker); err != nil {
		return Marker{}, err
	}
	if err := marker.validate(); err != nil {
		return Marker{}, err
	}
	return marker, nil
}

func (marker Marker) validate() error {
	switch marker.State {
	case StateIdle, StateChecking, StateApplying, StateFailed, StateCompleted:
	default:
		return fmt.Errorf("unknown migration status state %q", truncate(marker.State, maximumCodeBytes))
	}
	for _, field := range []struct {
		value   string
		maximum int
	}{
		{marker.Phase, maximumCodeBytes}, {marker.FailureCode, maximumCodeBytes},
		{marker.MigrationName, maximumNameBytes}, {marker.ApplicationVersion, maximumBuildBytes},
		{marker.BuildID, maximumBuildBytes}, {marker.State, maximumCodeBytes},
	} {
		if len(field.value) > field.maximum {
			return errors.New("migration status field exceeds its bound")
		}
	}
	for _, version := range []int{marker.SourceSchema, marker.TargetSchema, marker.CurrentSchema, marker.CurrentMigration} {
		if version < 0 || version > maximumVersion {
			return errors.New("migration status version is out of range")
		}
	}
	return nil
}
