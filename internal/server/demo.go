package server

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"beat/internal/domain"
	"beat/internal/version"
)

type demoNode struct {
	AgentID  string
	Hostname string
	Address  string
	OS       string
	Kernel   string
	Cores    int
	CPUModel string
	Memory   uint64
	Disk     uint64
	BaseCPU  float64
	BaseMem  float64
	BaseDisk float64
	Labels   map[string]string
	Offline  bool
}

type demoGroup struct {
	ID          string
	Name        string
	Description string
	Color       string
	AgentIDs    []string
}

var demoNodes = []demoNode{
	{AgentID: "demo-agent-01", Hostname: "edge-sh-01", Address: "10.20.1.11", OS: "Ubuntu 24.04 LTS", Kernel: "6.8.0-51-generic", Cores: 16, CPUModel: "Intel Xeon Gold 6338", Memory: 64 << 30, Disk: 1 << 40, BaseCPU: 42, BaseMem: 63, BaseDisk: 47, Labels: map[string]string{"region": "cn-east", "role": "edge", "env": "production"}},
	{AgentID: "demo-agent-02", Hostname: "edge-sh-02", Address: "10.20.1.12", OS: "Ubuntu 24.04 LTS", Kernel: "6.8.0-51-generic", Cores: 16, CPUModel: "Intel Xeon Gold 6338", Memory: 64 << 30, Disk: 1 << 40, BaseCPU: 35, BaseMem: 58, BaseDisk: 52, Labels: map[string]string{"region": "cn-east", "role": "edge", "env": "production"}},
	{AgentID: "demo-agent-03", Hostname: "gateway-bj-01", Address: "10.30.2.21", OS: "Debian 12", Kernel: "6.1.0-28-amd64", Cores: 32, CPUModel: "AMD EPYC 7543", Memory: 128 << 30, Disk: 2 << 40, BaseCPU: 67, BaseMem: 72, BaseDisk: 61, Labels: map[string]string{"region": "cn-north", "role": "gateway", "env": "production"}},
	{AgentID: "demo-agent-04", Hostname: "gateway-bj-02", Address: "10.30.2.22", OS: "Debian 12", Kernel: "6.1.0-28-amd64", Cores: 32, CPUModel: "AMD EPYC 7543", Memory: 128 << 30, Disk: 2 << 40, BaseCPU: 54, BaseMem: 66, BaseDisk: 58, Labels: map[string]string{"region": "cn-north", "role": "gateway", "env": "production"}},
	{AgentID: "demo-agent-05", Hostname: "compute-gz-01", Address: "10.40.3.31", OS: "Rocky Linux 9.4", Kernel: "5.14.0-427.el9", Cores: 64, CPUModel: "AMD EPYC 9654", Memory: 256 << 30, Disk: 4 << 40, BaseCPU: 73, BaseMem: 81, BaseDisk: 69, Labels: map[string]string{"region": "cn-south", "role": "compute", "env": "production"}},
	{AgentID: "demo-agent-06", Hostname: "compute-gz-02", Address: "10.40.3.32", OS: "Rocky Linux 9.4", Kernel: "5.14.0-427.el9", Cores: 64, CPUModel: "AMD EPYC 9654", Memory: 256 << 30, Disk: 4 << 40, BaseCPU: 58, BaseMem: 75, BaseDisk: 71, Labels: map[string]string{"region": "cn-south", "role": "compute", "env": "production"}},
	{AgentID: "demo-agent-07", Hostname: "store-hz-01", Address: "10.50.4.41", OS: "openEuler 24.03", Kernel: "6.6.0-72.oe2403", Cores: 24, CPUModel: "Intel Xeon Platinum 8468", Memory: 192 << 30, Disk: 8 << 40, BaseCPU: 48, BaseMem: 69, BaseDisk: 83, Labels: map[string]string{"region": "cn-east", "role": "storage", "env": "production"}},
	{AgentID: "demo-agent-08", Hostname: "store-hz-02", Address: "10.50.4.42", OS: "openEuler 24.03", Kernel: "6.6.0-72.oe2403", Cores: 24, CPUModel: "Intel Xeon Platinum 8468", Memory: 192 << 30, Disk: 8 << 40, BaseCPU: 44, BaseMem: 64, BaseDisk: 91, Labels: map[string]string{"region": "cn-east", "role": "storage", "env": "production"}},
	{AgentID: "demo-agent-09", Hostname: "ci-runner-01", Address: "10.60.5.51", OS: "Ubuntu 22.04 LTS", Kernel: "5.15.0-119-generic", Cores: 8, CPUModel: "Intel Xeon E-2378", Memory: 32 << 30, Disk: 512 << 30, BaseCPU: 88, BaseMem: 77, BaseDisk: 44, Labels: map[string]string{"region": "cn-east", "role": "ci", "env": "testing"}},
	{AgentID: "demo-agent-10", Hostname: "ci-runner-02", Address: "10.60.5.52", OS: "Ubuntu 22.04 LTS", Kernel: "5.15.0-119-generic", Cores: 8, CPUModel: "Intel Xeon E-2378", Memory: 32 << 30, Disk: 512 << 30, BaseCPU: 24, BaseMem: 41, BaseDisk: 39, Labels: map[string]string{"region": "cn-east", "role": "ci", "env": "testing"}},
	{AgentID: "demo-agent-11", Hostname: "backup-wx-01", Address: "10.70.6.61", OS: "Debian 12", Kernel: "6.1.0-28-amd64", Cores: 16, CPUModel: "Intel Xeon Silver 4314", Memory: 64 << 30, Disk: 12 << 40, BaseCPU: 12, BaseMem: 34, BaseDisk: 76, Labels: map[string]string{"region": "cn-east", "role": "backup", "env": "production"}, Offline: true},
	{AgentID: "demo-agent-12", Hostname: "monitor-cd-01", Address: "10.80.7.71", OS: "Ubuntu 24.04 LTS", Kernel: "6.8.0-51-generic", Cores: 8, CPUModel: "Intel Xeon E-2378", Memory: 32 << 30, Disk: 512 << 30, BaseCPU: 18, BaseMem: 39, BaseDisk: 33, Labels: map[string]string{"region": "cn-west", "role": "observability", "env": "production"}, Offline: true},
}

var demoGroups = []demoGroup{
	{ID: "demo-group-edge", Name: "边缘接入", Description: "华东区域边缘代理与入口节点", Color: "blue", AgentIDs: []string{"demo-agent-01", "demo-agent-02"}},
	{ID: "demo-group-gateway", Name: "核心网关", Description: "华北核心流量网关与转发节点", Color: "cyan", AgentIDs: []string{"demo-agent-03", "demo-agent-04"}},
	{ID: "demo-group-compute", Name: "计算集群", Description: "华南弹性计算与任务执行资源池", Color: "violet", AgentIDs: []string{"demo-agent-05", "demo-agent-06"}},
	{ID: "demo-group-storage", Name: "数据存储", Description: "对象存储与高容量数据节点", Color: "emerald", AgentIDs: []string{"demo-agent-07", "demo-agent-08"}},
	{ID: "demo-group-ci", Name: "构建环境", Description: "持续集成与发布流水线节点", Color: "amber", AgentIDs: []string{"demo-agent-09", "demo-agent-10"}},
}

func (s *Server) seedDemo(ctx context.Context) error {
	count, err := s.store.NodeCount(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if count == 0 {
		for index, template := range demoNodes {
			deleted, err := s.store.IsAgentDeleted(ctx, template.AgentID)
			if err != nil {
				return err
			}
			if deleted {
				continue
			}
			points := make([]domain.Metrics, 0, 288)
			for offset := 24 * time.Hour; offset >= 0; offset -= 5 * time.Minute {
				points = append(points, demoMetrics(template, now.Add(-offset), index, int(offset.Minutes())))
			}
			report := demoReport(template, points[len(points)-1])
			insertedAt := now
			if template.Offline {
				insertedAt = now.Add(-10 * time.Minute)
			}
			node, err := s.store.UpsertNode(ctx, report, insertedAt)
			if err != nil {
				return err
			}
			if err := s.tsdb.AppendBatch(ctx, node.ID, points); err != nil {
				return fmt.Errorf("seed demo metrics %s: %w", template.Hostname, err)
			}
		}
		s.logger.Info("demo_data_seeded", "nodes", len(demoNodes))
	}
	return s.seedDemoGroups(ctx, now)
}

func (s *Server) seedDemoGroups(ctx context.Context, now time.Time) error {
	groups, err := s.store.ListGroups(ctx, now)
	if err != nil {
		return err
	}
	if len(groups) > 0 {
		return nil
	}
	for _, template := range demoGroups {
		if _, err := s.store.CreateGroup(ctx, template.ID, template.Name, template.Description, template.Color, now); err != nil {
			return fmt.Errorf("seed demo group %s: %w", template.Name, err)
		}
	}
	nodes, err := s.store.ListNodes(ctx, "", now)
	if err != nil {
		return err
	}
	nodeByAgent := make(map[string]domain.Node, len(nodes))
	for _, node := range nodes {
		nodeByAgent[node.AgentID] = node
	}
	for _, template := range demoGroups {
		nodeIDs := make([]string, 0, len(template.AgentIDs))
		for _, agentID := range template.AgentIDs {
			node, ok := nodeByAgent[agentID]
			if ok && node.GroupID == "" {
				nodeIDs = append(nodeIDs, node.ID)
			}
		}
		if len(nodeIDs) == 0 {
			continue
		}
		if err := s.store.SetGroupMembers(ctx, template.ID, nodeIDs, now); err != nil {
			return fmt.Errorf("seed demo group members %s: %w", template.Name, err)
		}
	}
	s.logger.Info("demo_node_groups_seeded", "groups", len(demoGroups))
	return nil
}

func (s *Server) runDemoLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for index, template := range demoNodes {
				if template.Offline {
					continue
				}
				deleted, err := s.store.IsAgentDeleted(ctx, template.AgentID)
				if err != nil {
					s.logger.Warn("demo_tombstone_check_failed", "agent_id", template.AgentID, "error", err)
					continue
				}
				if deleted {
					continue
				}
				metrics := demoMetrics(template, now.UTC(), index, int(now.Unix()/60)%1440)
				report := demoReport(template, metrics)
				node, err := s.store.UpsertNode(ctx, report, now.UTC())
				if err != nil {
					s.logger.Warn("demo_upsert_failed", "node", template.Hostname, "error", err)
					continue
				}
				if err := s.tsdb.Append(ctx, node.ID, metrics); err != nil {
					s.logger.Warn("demo_append_failed", "node", template.Hostname, "error", err)
					continue
				}
			}
		}
	}
}

func demoReport(template demoNode, metrics domain.Metrics) domain.Report {
	return domain.Report{
		AgentID: template.AgentID, Hostname: template.Hostname, Address: template.Address,
		OS: template.OS, Kernel: template.Kernel, Arch: "amd64",
		AgentVersion: version.Version, IntervalSeconds: 15, Labels: template.Labels,
		CPUCores: template.Cores, CPUModel: template.CPUModel, Metrics: metrics,
		Interfaces: []domain.NetworkInterface{
			{Name: "eth0", Addresses: []string{template.Address + "/24"}, MAC: "52:54:00:12:34:56", RxBytes: 1849203301, TxBytes: 938200112},
			{Name: "lo", Addresses: []string{"127.0.0.1/8"}},
		},
	}
}

func demoMetrics(template demoNode, ts time.Time, index, minute int) domain.Metrics {
	rng := rand.New(rand.NewSource(int64(index*10000 + minute)))
	wave := math.Sin(float64(minute)/43+float64(index)) * 7
	cpu := clamp(template.BaseCPU+wave+rng.Float64()*9-4.5, 3, 98)
	memory := clamp(template.BaseMem+math.Sin(float64(minute)/91+float64(index))*3+rng.Float64()*2, 18, 97)
	disk := clamp(template.BaseDisk+float64(minute)/10000, 20, 96)
	loadBase := cpu / 100 * float64(template.Cores) * 0.8
	rx := (8 + rng.Float64()*18 + math.Max(0, math.Sin(float64(minute)/17))*12) * 1024 * 1024
	tx := (4 + rng.Float64()*9 + math.Max(0, math.Cos(float64(minute)/21))*7) * 1024 * 1024
	return domain.Metrics{
		Timestamp: ts, CPUUsage: round(cpu, 2), CPUUser: round(cpu*0.68, 2), CPUSystem: round(cpu*0.24, 2), CPUIowait: round(cpu*0.04, 2),
		MemoryUsedBytes: uint64(float64(template.Memory) * memory / 100), MemoryTotalBytes: template.Memory, MemoryUsage: round(memory, 2),
		DiskUsedBytes: uint64(float64(template.Disk) * disk / 100), DiskTotalBytes: template.Disk, DiskUsage: round(disk, 2),
		NetworkRxBps: round(rx, 0), NetworkTxBps: round(tx, 0),
		Load1: round(loadBase+rng.Float64()*0.8, 2), Load5: round(loadBase*0.88, 2), Load15: round(loadBase*0.75, 2),
		Processes: 180 + int(rng.Float64()*80), TCPConnections: 340 + int(rng.Float64()*180), UptimeSeconds: 86400*17 + int64(minute*60),
	}
}

func clamp(value, min, max float64) float64 {
	return math.Max(min, math.Min(max, value))
}
