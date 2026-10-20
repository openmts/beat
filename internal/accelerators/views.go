package accelerators

import (
	"context"
	"slices"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (s *Service) Node(ctx context.Context, actor store.AcceleratorActor, nodeID string) (model.NodeAcceleratorsAdmin, error) {
	if err := s.inventory.AuthorizeRead(ctx, actor, nodeID); err != nil {
		return model.NodeAcceleratorsAdmin{}, err
	}
	result := model.NodeAcceleratorsAdmin{State: "unavailable", ErrorCode: "not_reported"}
	var err error
	result.PublicVisible, err = s.inventory.PublicVisible(ctx, nodeID)
	if err != nil {
		return result, err
	}
	result.Policy, err = s.inventory.Policy(ctx, actor, store.AcceleratorPolicyScope{Kind: "node", ID: nodeID})
	if err != nil {
		return result, err
	}
	snapshot, err := s.inventory.State(ctx, nodeID)
	if err != nil {
		return result, err
	}
	result.Devices, err = s.devices(ctx, nodeID, false)
	if err != nil {
		return result, err
	}
	if snapshot != nil {
		result.State, result.ErrorCode, result.CollectorVersion, result.ObservedAt = snapshot.State, snapshot.ErrorCode, snapshot.CollectorVersion, &snapshot.CapturedAt
	}
	if !result.Policy.Enabled {
		result.State = "disabled"
		result.ErrorCode = "policy_disabled"
	}
	return result, nil
}

// PublicVisible 返回节点在指标页展示 GPU 区域的开关。
func (s *Service) PublicVisible(ctx context.Context, nodeID string) (bool, error) {
	return s.inventory.PublicVisible(ctx, nodeID)
}

// SetVisibility 保存节点级展示开关。
func (s *Service) SetVisibility(ctx context.Context, actor store.AcceleratorActor, nodeID string, visible bool) error {
	return s.inventory.SetVisibility(ctx, actor, nodeID, visible)
}

func (s *Service) PublicAccelerators(ctx context.Context, nodeID string) ([]model.AcceleratorDevicePublic, error) {
	result := []model.AcceleratorDevicePublic{}
	public, err := s.inventory.PublicNode(ctx, nodeID)
	if err != nil || !public {
		return result, err
	}
	devices, err := s.devices(ctx, nodeID, true)
	if err != nil {
		return nil, err
	}
	for _, device := range devices {
		if device.InactiveAt == nil {
			result = append(result, device.AcceleratorDevicePublic)
		}
	}
	return result, nil
}

func (s *Service) devices(ctx context.Context, nodeID string, public bool) ([]model.AcceleratorDeviceAdmin, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	devices, err := s.inventory.Devices(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.inventory.State(ctx, nodeID)
	if err != nil || snapshot == nil {
		return devices, err
	}
	policy, err := s.inventory.AgentPolicy(ctx, nodeID, snapshot.InstallationEnabled)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled && public {
		return []model.AcceleratorDeviceAdmin{}, nil
	}
	allowed, err := s.allowedMetrics(ctx, public)
	if err != nil {
		return nil, err
	}
	values, err := s.metrics.AcceleratorValues(ctx, store.AcceleratorValueQuery{NodeID: nodeID, Snapshot: *snapshot, Allowed: allowed})
	if err != nil {
		return nil, err
	}
	applyDeviceValues(devices, deviceView{snapshot: *snapshot, enabled: policy.Enabled, allowed: allowed, values: values, now: s.now()})
	return devices, nil
}

func (s *Service) allowedMetrics(ctx context.Context, public bool) (map[string]bool, error) {
	if public {
		return s.catalog.PublicKeys(ctx)
	}
	return s.catalog.BuiltinPersistence(ctx)
}

func acceleratorStale(now, captured time.Time, cadence int64) bool {
	return now.Sub(captured) > max(3*time.Duration(cadence)*time.Millisecond, 30*time.Second)
}

type deviceView struct {
	snapshot model.AcceleratorInventorySnapshot
	enabled  bool
	allowed  map[string]bool
	values   map[string]map[string]float64
	now      time.Time
}

func applyDeviceValues(devices []model.AcceleratorDeviceAdmin, input deviceView) {
	for index := range devices {
		device := &devices[index]
		device.CadenceMS = input.snapshot.CadenceMS
		device.Capabilities = slices.DeleteFunc(device.Capabilities, func(key string) bool { return !input.allowed[key] })
		if current, ok := input.values[device.DeviceID]; ok {
			device.Metrics = current
			at := input.snapshot.CapturedAt
			device.CapturedAt = &at
			device.Stale = acceleratorStale(input.now, at, input.snapshot.CadenceMS) || device.InactiveAt != nil || !input.enabled
		}
	}
}
