package metricquery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

// MaximumLatestSeries 限制一次即时查询的序列数：即时查询不做时间分桶，但同样要
// 有硬上限，避免一次请求把整机的指标全量拉出来。
const MaximumLatestSeries = 200

// LatestRequest 是「取最新样本」的即时查询请求，字段与区间查询保持同一套命名。
type LatestRequest struct {
	Series []LatestSeriesRequest `json:"series"`
}

type LatestSeriesRequest struct {
	RefID  string `json:"ref_id"`
	NodeID string `json:"node_id"`
	Metric string `json:"metric"`
}

// LatestSeries 是一条即时查询结果：State 为 ok 时 Value/SampleAt 才有意义，
// empty 表示该节点该指标没有可用样本。
type LatestSeries struct {
	RefID    string     `json:"ref_id"`
	NodeID   string     `json:"node_id"`
	Metric   string     `json:"metric"`
	Label    string     `json:"label"`
	Unit     string     `json:"unit"`
	State    string     `json:"state"`
	Value    *float64   `json:"value,omitempty"`
	SampleAt *time.Time `json:"sample_at,omitempty"`
}

type LatestResponse struct {
	Series []LatestSeries `json:"series"`
}

// LatestScan 是存储层读取最新样本的最小输入。
type LatestScan struct {
	NodeID string
	Metric string
}

// LatestValue 是存储层的结果；Found 为 false 表示窗口内没有任何样本。
type LatestValue struct {
	Found    bool
	SampleAt time.Time
	Value    float64
}

// LatestReader 由支持即时读取的实现提供；Service 在构造时按能力自动接线。
type LatestReader interface {
	ReadMetricLatest(context.Context, LatestScan) (LatestValue, error)
}

func ValidateLatestRequest(request LatestRequest) error {
	if len(request.Series) == 0 || len(request.Series) > MaximumLatestSeries {
		return fmt.Errorf("%w: one to %d series are required", ErrInvalid, MaximumLatestSeries)
	}
	seen := make(map[string]bool, len(request.Series))
	for _, selection := range request.Series {
		if !validIdentifier(selection.RefID, MaximumReferenceBytes) || seen[selection.RefID] {
			return fmt.Errorf("%w: series references must be unique and bounded", ErrInvalid)
		}
		if !validIdentifier(selection.NodeID, MaximumNodeIDBytes) || !metriccatalog.ValidKey(selection.Metric) {
			return fmt.Errorf("%w: invalid node or metric key", ErrInvalid)
		}
		seen[selection.RefID] = true
	}
	return nil
}

type latestPlan struct {
	scan       LatestScan
	definition model.MetricDefinition
	refID      string
}

// Latest 返回每条请求序列的当前样本。校验、节点可见性、目录启用状态与准入预算
// 都与区间查询一致，因此即时查询不能绕过任何既有边界。
func (service *Service) Latest(ctx context.Context, request LatestRequest, access Access) (LatestResponse, error) {
	if service == nil {
		return LatestResponse{}, ErrUnavailable
	}
	if err := ValidateLatestRequest(request); err != nil {
		return LatestResponse{}, err
	}
	if service.catalog == nil || service.nodes == nil || service.latest == nil {
		return LatestResponse{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	response, err := service.latestWithin(ctx, request, access)
	return response, timeoutError(ctx, err)
}

// latestWithin 在已经装好 5s 预算的上下文里执行一次即时查询。
func (service *Service) latestWithin(ctx context.Context, request LatestRequest, access Access) (LatestResponse, error) {
	release, err := service.budget.acquire(ctx, access.ClientKey)
	if err != nil {
		return LatestResponse{}, err
	}
	defer release()
	plans, err := service.prepareLatest(ctx, request, access)
	if err != nil {
		return LatestResponse{}, err
	}
	response := LatestResponse{Series: make([]LatestSeries, 0, len(plans))}
	for _, plan := range plans {
		series, err := service.readLatest(ctx, plan)
		if err != nil {
			return LatestResponse{}, err
		}
		response.Series = append(response.Series, series)
	}
	return response, nil
}

func (service *Service) prepareLatest(ctx context.Context, request LatestRequest, access Access) ([]latestPlan, error) {
	definitions := make(map[string]*model.MetricDefinition)
	nodes := make(map[string]bool)
	plans := make([]latestPlan, 0, len(request.Series))
	for _, selection := range request.Series {
		if !nodes[selection.NodeID] {
			if err := service.checkNode(ctx, selection.NodeID, access.PublicOnly); err != nil {
				return nil, err
			}
			nodes[selection.NodeID] = true
		}
		if definitions[selection.Metric] == nil {
			definition, err := service.catalog.Get(ctx, selection.Metric)
			if err != nil {
				return nil, errors.Join(ErrUnavailable, err)
			}
			if definition == nil || !definition.Enabled || (access.PublicOnly && !definition.Public) {
				return nil, ErrInvalid
			}
			definitions[selection.Metric] = definition
		}
		plans = append(plans, latestPlan{scan: LatestScan{NodeID: selection.NodeID, Metric: selection.Metric}, definition: *definitions[selection.Metric], refID: selection.RefID})
	}
	return plans, nil
}

func (service *Service) readLatest(ctx context.Context, plan latestPlan) (LatestSeries, error) {
	value, err := service.latest.ReadMetricLatest(ctx, plan.scan)
	if err != nil {
		if errors.Is(err, ErrCapacity) || errors.Is(err, ErrArithmetic) {
			return LatestSeries{}, err
		}
		return LatestSeries{}, errors.Join(ErrUnavailable, err)
	}
	series := LatestSeries{RefID: plan.refID, NodeID: plan.scan.NodeID, Metric: plan.definition.Key, Label: plan.definition.Label, Unit: plan.definition.Unit, State: "empty"}
	if !value.Found {
		return series, nil
	}
	if !finite(value.Value) || value.SampleAt.IsZero() {
		return LatestSeries{}, ErrUnavailable
	}
	sample := value.Value
	sampleAt := value.SampleAt.UTC()
	series.State, series.Value, series.SampleAt = "ok", &sample, &sampleAt
	return series, nil
}
