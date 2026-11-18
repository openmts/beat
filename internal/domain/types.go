package domain

import (
	"encoding/json"
	"time"
)

type NodeStatus string

const (
	NodeOnline  NodeStatus = "online"
	NodeWarning NodeStatus = "warning"
	NodeOffline NodeStatus = "offline"
)

type NetworkInterface struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	MAC       string   `json:"mac,omitempty"`
	RxBytes   uint64   `json:"rxBytes"`
	TxBytes   uint64   `json:"txBytes"`
}

type Metrics struct {
	Timestamp        time.Time `json:"timestamp"`
	CPUUsage         float64   `json:"cpuUsage"`
	CPUUser          float64   `json:"cpuUser"`
	CPUSystem        float64   `json:"cpuSystem"`
	CPUIowait        float64   `json:"cpuIowait"`
	MemoryUsedBytes  uint64    `json:"memoryUsedBytes"`
	MemoryTotalBytes uint64    `json:"memoryTotalBytes"`
	MemoryUsage      float64   `json:"memoryUsage"`
	SwapUsedBytes    uint64    `json:"swapUsedBytes"`
	DiskUsedBytes    uint64    `json:"diskUsedBytes"`
	DiskTotalBytes   uint64    `json:"diskTotalBytes"`
	DiskUsage        float64   `json:"diskUsage"`
	NetworkRxBps     float64   `json:"networkRxBps"`
	NetworkTxBps     float64   `json:"networkTxBps"`
	Load1            float64   `json:"load1"`
	Load5            float64   `json:"load5"`
	Load15           float64   `json:"load15"`
	Processes        int       `json:"processes"`
	TCPConnections   int       `json:"tcpConnections"`
	UptimeSeconds    int64     `json:"uptimeSeconds"`
}

type Node struct {
	ID                       string             `json:"id"`
	AgentID                  string             `json:"agentId"`
	Hostname                 string             `json:"hostname"`
	GroupID                  string             `json:"groupId"`
	GroupName                string             `json:"groupName"`
	GroupColor               string             `json:"groupColor"`
	Address                  string             `json:"address"`
	OS                       string             `json:"os"`
	Kernel                   string             `json:"kernel"`
	Arch                     string             `json:"arch"`
	AgentVersion             string             `json:"agentVersion"`
	Status                   NodeStatus         `json:"status"`
	Labels                   map[string]string  `json:"labels"`
	Interfaces               []NetworkInterface `json:"-"`
	CPUCores                 int                `json:"cpuCores"`
	CPUModel                 string             `json:"cpuModel"`
	TotalMemoryBytes         uint64             `json:"totalMemoryBytes"`
	TotalDiskBytes           uint64             `json:"totalDiskBytes"`
	CollectorIntervalSeconds int                `json:"collectorIntervalSeconds"`
	FirstSeen                time.Time          `json:"firstSeen"`
	LastSeen                 time.Time          `json:"lastSeen"`
	Metrics                  Metrics            `json:"metrics"`
}

type NodeGroup struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	NodeCount    int       `json:"nodeCount"`
	OnlineCount  int       `json:"onlineCount"`
	WarningCount int       `json:"warningCount"`
	OfflineCount int       `json:"offlineCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Report struct {
	AgentID         string             `json:"agentId"`
	Hostname        string             `json:"hostname"`
	Address         string             `json:"address"`
	OS              string             `json:"os"`
	Kernel          string             `json:"kernel"`
	Arch            string             `json:"arch"`
	AgentVersion    string             `json:"agentVersion"`
	IntervalSeconds int                `json:"intervalSeconds"`
	Labels          map[string]string  `json:"labels"`
	Interfaces      []NetworkInterface `json:"interfaces"`
	CPUCores        int                `json:"cpuCores"`
	CPUModel        string             `json:"cpuModel"`
	Metrics         Metrics            `json:"metrics"`
}

type Agent struct {
	ID              string     `json:"id"`
	NodeID          string     `json:"nodeId"`
	NodeName        string     `json:"nodeName"`
	Version         string     `json:"version"`
	Status          NodeStatus `json:"status"`
	IntervalSeconds int        `json:"intervalSeconds"`
	LastSeen        time.Time  `json:"lastSeen"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type AgentUpgradeArtifact struct {
	Arch        string `json:"arch"`
	Version     string `json:"version"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"downloadUrl"`
	Available   bool   `json:"available"`
}

type AgentUpgradeItem struct {
	ID             string     `json:"id"`
	BatchID        string     `json:"batchId"`
	AgentID        string     `json:"agentId"`
	NodeID         string     `json:"nodeId"`
	NodeName       string     `json:"nodeName"`
	Arch           string     `json:"arch"`
	FromVersion    string     `json:"fromVersion"`
	ToVersion      string     `json:"toVersion"`
	ArtifactSHA256 string     `json:"artifactSha256"`
	ArtifactSize   int64      `json:"artifactSize"`
	ArtifactURL    string     `json:"artifactUrl"`
	Status         string     `json:"status"`
	Message        string     `json:"message"`
	Attempt        int        `json:"attempt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeliveredAt    *time.Time `json:"deliveredAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

type AgentUpgradeBatch struct {
	ID            string             `json:"id"`
	TargetVersion string             `json:"targetVersion"`
	Status        string             `json:"status"`
	Total         int                `json:"total"`
	Queued        int                `json:"queued"`
	Delivered     int                `json:"delivered"`
	Succeeded     int                `json:"succeeded"`
	Failed        int                `json:"failed"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
	CompletedAt   *time.Time         `json:"completedAt,omitempty"`
	Items         []AgentUpgradeItem `json:"items"`
}

type AgentUpgradeOverview struct {
	TargetVersion string                 `json:"targetVersion"`
	ReleaseTag    string                 `json:"releaseTag"`
	ReleaseURL    string                 `json:"releaseUrl"`
	Repository    string                 `json:"repository"`
	Artifacts     []AgentUpgradeArtifact `json:"artifacts"`
	TotalNodes    int                    `json:"totalNodes"`
	UpToDate      int                    `json:"upToDate"`
	Upgradable    int                    `json:"upgradable"`
	Online        int                    `json:"online"`
	Offline       int                    `json:"offline"`
	Unsupported   int                    `json:"unsupported"`
	ActiveBatch   *AgentUpgradeBatch     `json:"activeBatch,omitempty"`
	RecentBatches []AgentUpgradeBatch    `json:"recentBatches"`
}

type AgentTask struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"taskId,omitempty"`
	AgentID        string          `json:"agentId"`
	NodeID         string          `json:"nodeId"`
	Kind           string          `json:"kind"`
	Name           string          `json:"name,omitempty"`
	Script         string          `json:"script,omitempty"`
	Config         json.RawMessage `json:"config,omitempty"`
	TimeoutSeconds int             `json:"timeoutSeconds,omitempty"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type TaskDefinition struct {
	ID              string          `json:"id"`
	Slug            string          `json:"slug"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Source          string          `json:"source"`
	Kind            string          `json:"kind"`
	Script          string          `json:"script"`
	Config          json.RawMessage `json:"config"`
	IntervalSeconds int             `json:"intervalSeconds"`
	TimeoutSeconds  int             `json:"timeoutSeconds"`
	Enabled         bool            `json:"enabled"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	LastRunAt       *time.Time      `json:"lastRunAt,omitempty"`
	LastStatus      string          `json:"lastStatus,omitempty"`
	RunCount        int             `json:"runCount"`
	SuccessCount    int             `json:"successCount"`
	FailureCount    int             `json:"failureCount"`
}

type TaskRun struct {
	ID             string     `json:"id"`
	TaskID         string     `json:"taskId"`
	TaskName       string     `json:"taskName"`
	TaskSource     string     `json:"taskSource"`
	AgentID        string     `json:"agentId"`
	NodeID         string     `json:"nodeId"`
	NodeName       string     `json:"nodeName"`
	Script         string     `json:"script,omitempty"`
	TimeoutSeconds int        `json:"timeoutSeconds"`
	Status         string     `json:"status"`
	ScheduledAt    time.Time  `json:"scheduledAt"`
	DeliveredAt    *time.Time `json:"deliveredAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	ExitCode       int        `json:"exitCode"`
	DurationMs     int64      `json:"durationMs"`
	Output         string     `json:"output"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type SeriesPoint struct {
	Timestamp      time.Time `json:"timestamp"`
	CPUUsage       float64   `json:"cpuUsage"`
	CPUUser        float64   `json:"cpuUser"`
	CPUSystem      float64   `json:"cpuSystem"`
	MemoryUsage    float64   `json:"memoryUsage"`
	DiskUsage      float64   `json:"diskUsage"`
	NetworkRxBps   float64   `json:"networkRxBps"`
	NetworkTxBps   float64   `json:"networkTxBps"`
	Load1          float64   `json:"load1"`
	Load5          float64   `json:"load5"`
	Load15         float64   `json:"load15"`
	Processes      float64   `json:"processes"`
	TCPConnections float64   `json:"tcpConnections"`
}

type NetworkProbePoint struct {
	Timestamp        time.Time
	TaskID           string
	TaskName         string
	Carrier          string
	Label            string
	LossAvailable    bool
	LossPercent      float64
	Sent             int
	Received         int
	Lost             int
	Failed           int
	LatencyAvailable bool
	LatencyMinMs     float64
	LatencyAvgMs     float64
	LatencyMaxMs     float64
	LatencyP50Ms     float64
	LatencyP95Ms     float64
	LatencyJitterMs  float64
}

type NetworkProbeResponse struct {
	NodeID      string                   `json:"nodeId"`
	From        time.Time                `json:"from"`
	To          time.Time                `json:"to"`
	StepSeconds int                      `json:"stepSeconds"`
	Tasks       []NetworkProbeTaskSeries `json:"tasks"`
}

type NetworkProbeTaskSeries struct {
	TaskID     string                     `json:"taskId"`
	TaskName   string                     `json:"taskName"`
	LastRunAt  time.Time                  `json:"lastRunAt"`
	Timestamps []time.Time                `json:"timestamps"`
	Targets    []NetworkProbeTargetSeries `json:"targets"`
}

type NetworkProbeTargetSeries struct {
	Carrier         string     `json:"carrier"`
	Label           string     `json:"label"`
	LossPercent     []*float64 `json:"lossPercent"`
	LatencyAvgMs    []*float64 `json:"latencyAvgMs"`
	LatencyP95Ms    []*float64 `json:"latencyP95Ms"`
	LatencyJitterMs []*float64 `json:"latencyJitterMs"`
}

type FleetPoint struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUUsage    float64   `json:"cpuUsage"`
	MemoryUsage float64   `json:"memoryUsage"`
	DiskUsage   float64   `json:"diskUsage"`
	NetworkRx   float64   `json:"networkRx"`
	NetworkTx   float64   `json:"networkTx"`
}

func (n Node) Online(now time.Time, timeout time.Duration) bool {
	return now.Sub(n.LastSeen) <= timeout
}
