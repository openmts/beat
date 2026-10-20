package metricquery

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/beat/backend/internal/model"
)

type CatalogReader interface {
	Get(context.Context, string) (*model.MetricDefinition, error)
}

type NodeReader interface {
	GetNode(context.Context, string) (*model.Node, error)
	GetPublicNode(context.Context, string) (*model.Node, error)
}

type SeriesReader interface {
	ReadMetricQuery(context.Context, Scan) (Values, error)
}

type Service struct {
	catalog CatalogReader
	nodes   NodeReader
	reader  SeriesReader
	latest  LatestReader
	budget  *admission
}

func (service *Service) ReadWithBudget(ctx context.Context, client string, read func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	release, err := service.budget.acquire(ctx, client)
	if err != nil {
		return err
	}
	defer release()
	return read(ctx)
}

func NewService(catalog CatalogReader, nodes NodeReader, reader SeriesReader) *Service {
	service := &Service{catalog: catalog, nodes: nodes, reader: reader, budget: newAdmission()}
	// 区间查询与即时查询共用同一份存储实现；只实现区间查询的读取器保持即时查询不可用。
	if latest, ok := reader.(LatestReader); ok {
		service.latest = latest
	}
	return service
}

func (service *Service) Query(ctx context.Context, request Request, access Access) (Response, error) {
	if service == nil {
		return Response{}, ErrUnavailable
	}
	interval, count, err := ValidateRequest(request)
	if err != nil {
		return Response{}, err
	}
	if service.catalog == nil || service.nodes == nil || service.reader == nil {
		return Response{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	response, err := service.queryWithin(ctx, request, access, interval, count)
	return response, timeoutError(ctx, err)
}

// queryWithin 在已经装好 5s 预算的上下文里执行一次区间查询。
func (service *Service) queryWithin(ctx context.Context, request Request, access Access, interval time.Duration, count int) (Response, error) {
	release, err := service.budget.acquire(ctx, access.ClientKey)
	if err != nil {
		return Response{}, err
	}
	defer release()
	plans, err := service.prepare(ctx, request, planOptions{access: access, interval: interval, count: count})
	if err != nil {
		return Response{}, err
	}
	response := Response{From: request.From.UTC(), To: request.To.UTC(), IntervalMS: interval.Milliseconds(), Series: make([]Series, 0, len(plans))}
	for _, plan := range plans {
		series, err := service.read(ctx, plan)
		if err != nil {
			return Response{}, err
		}
		response.Series = append(response.Series, series)
	}
	return response, nil
}

// timeoutError 把「本次查询用光了自己的预算」归一成 ErrTimeout：调用方据此回 429 并提示缩短时间范围，
// 而不是把超时误报成存储故障。取消（客户端断开）仍按不可用处理。
func timeoutError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, ErrTimeout) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	return err
}

type planOptions struct {
	access   Access
	interval time.Duration
	count    int
}

type seriesPlan struct {
	scan       Scan
	definition model.MetricDefinition
	refID      string
}

func (service *Service) prepare(ctx context.Context, request Request, options planOptions) ([]seriesPlan, error) {
	definitions := make(map[string]*model.MetricDefinition)
	nodes := make(map[string]bool)
	plans := make([]seriesPlan, 0, len(request.Series))
	for _, selection := range request.Series {
		if !nodes[selection.NodeID] {
			if err := service.checkNode(ctx, selection.NodeID, options.access.PublicOnly); err != nil {
				return nil, err
			}
			nodes[selection.NodeID] = true
		}
		definition := definitions[selection.Metric]
		if definition == nil {
			var err error
			definition, err = service.catalog.Get(ctx, selection.Metric)
			if err != nil {
				return nil, errors.Join(ErrUnavailable, err)
			}
			if definition == nil {
				return nil, ErrInvalid
			}
			definitions[selection.Metric] = definition
		}
		aggregation, err := selectMetricAggregation(*definition, selection.Aggregation, options.access.PublicOnly)
		if err != nil {
			return nil, err
		}
		vendor, deviceLabel, err := service.prepareDevice(ctx, selection, options.access.PublicOnly)
		if err != nil {
			return nil, err
		}
		scan := Scan{DeviceID: selection.DeviceID, Vendor: vendor, NodeID: selection.NodeID, Metric: selection.Metric, Aggregation: aggregation, From: request.From, To: request.To,
			Interval: options.interval, Buckets: options.count}
		planDefinition := *definition
		if deviceLabel != "" {
			planDefinition.Label += " · " + deviceLabel
		}
		plans = append(plans, seriesPlan{scan: scan, definition: planDefinition, refID: selection.RefID})
	}
	return plans, nil
}

func selectMetricAggregation(definition model.MetricDefinition, requested string, publicOnly bool) (string, error) {
	if !definition.Enabled || (publicOnly && !definition.Public) {
		return "", ErrInvalid
	}
	if requested == "" {
		requested = definition.DefaultAggregation
	}
	if !slices.Contains(definition.Aggregations, requested) {
		return "", ErrInvalid
	}
	return requested, nil
}

func (service *Service) checkNode(ctx context.Context, nodeID string, publicOnly bool) error {
	var node *model.Node
	var err error
	if publicOnly {
		node, err = service.nodes.GetPublicNode(ctx, nodeID)
	} else {
		node, err = service.nodes.GetNode(ctx, nodeID)
	}
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if node == nil {
		return ErrInvalid
	}
	return nil
}

func (service *Service) read(ctx context.Context, plan seriesPlan) (Series, error) {
	values, err := service.reader.ReadMetricQuery(ctx, plan.scan)
	if err != nil {
		if errors.Is(err, ErrCapacity) || errors.Is(err, ErrArithmetic) {
			return Series{}, err
		}
		return Series{}, errors.Join(ErrUnavailable, err)
	}
	if !validReadValues(plan.scan, values) {
		return Series{}, ErrUnavailable
	}
	series := Series{DeviceID: plan.scan.DeviceID, RefID: plan.refID, NodeID: plan.scan.NodeID, Metric: plan.definition.Key, Label: plan.definition.Label,
		Unit: plan.definition.Unit, Aggregation: plan.scan.Aggregation, AvailableFrom: values.AvailableFrom, State: "empty", Points: values.Points}
	if series.Aggregation == "rate" {
		series.Unit += "_per_second"
	}
	for _, point := range series.Points {
		if point.Value != nil {
			series.State = "ok"
			break
		}
	}
	return series, nil
}

func validReadValues(plan Scan, values Values) bool {
	if len(values.Points) != plan.Buckets {
		return false
	}
	for index, point := range values.Points {
		if point.Timestamp != plan.From.Add(time.Duration(index)*plan.Interval).UnixMilli() {
			return false
		}
		if point.Value != nil && !finite(*point.Value) {
			return false
		}
	}
	return true
}
