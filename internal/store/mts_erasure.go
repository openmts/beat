package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

// MetricErasurePlan 是一次冻结后的不可变计划：测量清单、清单摘要、截止时间与标签过滤。
type MetricErasurePlan struct {
	Measurements   []string
	Digest         string
	CutoffUnixNano int64
	NodeID         string
	TaskID         string
}

// MetricErasureSession 在整个 tombstone 发布期间持有 MTS 独占操作锁。
//
// 锁内不再调用任何会再次加锁的方法：备份、恢复暂存、保留清理与逻辑导出都被挡住，
// 因此外部永远看不到“删了一半”的快照。
type MetricErasureSession struct {
	store  *MTSStore
	unlock func()
	plan   MetricErasurePlan
	closed bool
}

// Plan 返回冻结的计划副本。
func (session *MetricErasureSession) Plan() MetricErasurePlan {
	plan := session.plan
	plan.Measurements = append([]string(nil), session.plan.Measurements...)
	return plan
}

// Close 释放独占锁；重复调用是安全的。
func (session *MetricErasureSession) Close() {
	if session == nil || session.closed {
		return
	}
	session.closed = true
	session.unlock()
}

// Erase 发布一个测量的 tombstone；空清单会在上面被拒绝，因此这里只做精确删除。
func (session *MetricErasureSession) Erase(ctx context.Context, measurement string) error {
	if session == nil || session.closed {
		return ErrMTSStoreClosed
	}
	return session.store.eraseMeasurementRange(ctx, measurement, session.plan)
}

// BeginMetricErasure 取得独占锁、flush 挂起写入并冻结清单与截止时间。
func (s *MTSStore) BeginMetricErasure(
	ctx context.Context,
	scope string,
	nodeID string,
	taskID string,
) (*MetricErasureSession, error) {
	if !model.ValidMetricErasureScope(scope) {
		return nil, model.ErrMetricErasureInvalid
	}
	unlock, err := s.lockMetricOperation(ctx, true)
	if err != nil {
		return nil, err
	}
	session := &MetricErasureSession{store: s, unlock: unlock}
	if err := s.ensureOpen(); err != nil {
		session.Close()
		return nil, err
	}
	if err := s.engine.Flush(ctx); err != nil {
		session.Close()
		return nil, fmt.Errorf("flush MTS before erasure: %w", err)
	}
	plan, err := s.metricErasurePlan(ctx, scope, nodeID, taskID)
	if err != nil {
		session.Close()
		return nil, err
	}
	session.plan = plan
	return session, nil
}

// metricErasurePlan 校验测量清单闭合并冻结截止时间。
func (s *MTSStore) metricErasurePlan(
	ctx context.Context,
	scope string,
	nodeID string,
	taskID string,
) (MetricErasurePlan, error) {
	known, err := s.metricErasureInventory(ctx)
	if err != nil {
		return MetricErasurePlan{}, err
	}
	selected, err := metricErasureSelection(scope, known, nodeID, taskID)
	if err != nil {
		return MetricErasurePlan{}, err
	}
	return MetricErasurePlan{Measurements: selected, Digest: metricErasureDigest(known),
		CutoffUnixNano: model.NowUTC().UnixNano(), NodeID: nodeID, TaskID: taskID}, nil
}

// metricErasureInventory 返回受管测量清单，并拒绝任何未知的 MTS 测量。
//
// 未知测量意味着有代码在没有登记的情况下写入时序数据；此时宁可让任务失败等待运维，
// 也不能悄悄留下永久无法擦除的历史。
func (s *MTSStore) metricErasureInventory(ctx context.Context) ([]string, error) {
	present, err := s.engine.ListMeasurements(ctx, "beat")
	if err != nil {
		return nil, fmt.Errorf("list MTS measurements: %w", err)
	}
	known, err := s.metricErasureKnownMeasurements(ctx)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(known))
	for _, measurement := range known {
		allowed[measurement] = true
	}
	unknown := make([]string, 0)
	sorted := append([]string(nil), present...)
	sort.Strings(sorted)
	for _, measurement := range sorted {
		if measurement != "" && !allowed[measurement] {
			unknown = append(unknown, measurement)
		}
	}
	if len(unknown) != 0 {
		return nil, fmt.Errorf("%w: %s", ErrMetricErasureInventory, strings.Join(unknown, ","))
	}
	return sortUniqueMeasurements(known), nil
}

// metricErasureKnownMeasurements 返回封闭清单：指标目录登记的测量加上不落目录的三类内建测量。
//
// 自定义指标的名称由用户决定、只登记在目录里；漏掉它们会让任何含自定义指标的节点
// 永久无法删除历史。所以目录存在时以目录为准，目录缺失时退回固定内建清单。
func (s *MTSStore) metricErasureKnownMeasurements(ctx context.Context) ([]string, error) {
	if s.catalog == nil {
		return managedMeasurements(), nil
	}
	catalog, err := s.catalog.Catalog(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("read metric catalog for erasure: %w", err)
	}
	known := make([]string, 0, len(catalog.Metrics)+3)
	for _, definition := range catalog.Metrics {
		known = append(known, definition.Key)
	}
	return append(known, networkProbeMeasurement, model.AgentReceiptMeasurement,
		model.AcceleratorSampleMeasurement), nil
}

// ErrMetricErasureInventory 表示 MTS 出现了未登记的测量，擦除必须失败关闭。
var ErrMetricErasureInventory = errors.New("unknown MTS measurement")

func sortUniqueMeasurements(measurements []string) []string {
	seen := make(map[string]bool, len(measurements))
	unique := make([]string, 0, len(measurements))
	for _, measurement := range measurements {
		if measurement == "" || seen[measurement] {
			continue
		}
		seen[measurement] = true
		unique = append(unique, measurement)
	}
	sort.Strings(unique)
	return unique
}

// metricErasureSelection 把封闭范围翻译成测量清单。
func metricErasureSelection(scope string, known []string, nodeID, taskID string) ([]string, error) {
	switch scope {
	case model.MetricErasureNodeHistory, model.MetricErasureDeleteNode:
		if nodeID == "" {
			return nil, model.ErrMetricErasureInvalid
		}
		return append([]string(nil), known...), nil
	case model.MetricErasureNetworkTaskHistory, model.MetricErasureDeleteNetworkTask:
		if taskID == "" {
			return nil, model.ErrMetricErasureInvalid
		}
		return []string{networkProbeMeasurement}, nil
	case model.MetricErasureFleetTelemetry:
		selected := make([]string, 0, len(known))
		for _, measurement := range known {
			if measurement != networkProbeMeasurement {
				selected = append(selected, measurement)
			}
		}
		return selected, nil
	default:
		return append([]string(nil), known...), nil
	}
}

func metricErasureDigest(known []string) string {
	sum := sha256.Sum256([]byte(strings.Join(known, "\n")))
	return hex.EncodeToString(sum[:])
}

func (s *MTSStore) eraseMeasurementRange(
	ctx context.Context,
	measurement string,
	plan MetricErasurePlan,
) error {
	tags := metricErasureTags(plan)
	if err := s.engine.Delete(ctx, mts.DeleteRequest{Database: "beat", Measurement: measurement,
		Tags: tags, StartTime: 0, EndTime: plan.CutoffUnixNano, Precision: mts.PrecisionNanosecond}); err != nil {
		return fmt.Errorf("erase MTS measurement %s: %w", measurement, err)
	}
	return nil
}

func metricErasureTags(plan MetricErasurePlan) map[string]string {
	if plan.NodeID != "" {
		return map[string]string{"node": plan.NodeID}
	}
	if plan.TaskID != "" {
		return map[string]string{"task": plan.TaskID}
	}
	return nil
}

// CompactMetricErasure 回收 tombstone 占用的磁盘；失败不回滚任何已发布 tombstone。
func (s *MTSStore) CompactMetricErasure(ctx context.Context) error {
	unlock, err := s.lockMetricOperation(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if _, err := s.engine.CompactWithResult(ctx); err != nil {
		return fmt.Errorf("compact MTS after erasure: %w", err)
	}
	return nil
}
