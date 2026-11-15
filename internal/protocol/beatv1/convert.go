package beatv1

import (
	"time"

	"beat/internal/domain"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func ReportFromDomain(report domain.Report) *Report {
	interfaces := make([]*NetworkInterface, 0, len(report.Interfaces))
	for _, item := range report.Interfaces {
		interfaces = append(interfaces, &NetworkInterface{
			Name: item.Name, Addresses: append([]string(nil), item.Addresses...), Mac: item.MAC,
			RxBytes: item.RxBytes, TxBytes: item.TxBytes,
		})
	}
	labels := make(map[string]string, len(report.Labels))
	for key, value := range report.Labels {
		labels[key] = value
	}
	return &Report{
		AgentId: report.AgentID, Hostname: report.Hostname, Address: report.Address,
		Os: report.OS, Kernel: report.Kernel, Arch: report.Arch,
		AgentVersion: report.AgentVersion, IntervalSeconds: int32(report.IntervalSeconds),
		Labels: labels, Interfaces: interfaces, CpuCores: int32(report.CPUCores),
		CpuModel: report.CPUModel, Metrics: MetricsFromDomain(report.Metrics),
	}
}

func ReportToDomain(report *Report) domain.Report {
	if report == nil {
		return domain.Report{}
	}
	interfaces := make([]domain.NetworkInterface, 0, len(report.Interfaces))
	for _, item := range report.Interfaces {
		interfaces = append(interfaces, domain.NetworkInterface{
			Name: item.Name, Addresses: append([]string(nil), item.Addresses...), MAC: item.Mac,
			RxBytes: item.RxBytes, TxBytes: item.TxBytes,
		})
	}
	labels := make(map[string]string, len(report.Labels))
	for key, value := range report.Labels {
		labels[key] = value
	}
	return domain.Report{
		AgentID: report.AgentId, Hostname: report.Hostname, Address: report.Address,
		OS: report.Os, Kernel: report.Kernel, Arch: report.Arch,
		AgentVersion: report.AgentVersion, IntervalSeconds: int(report.IntervalSeconds),
		Labels: labels, Interfaces: interfaces, CPUCores: int(report.CpuCores),
		CPUModel: report.CpuModel, Metrics: MetricsToDomain(report.Metrics),
	}
}

func MetricsFromDomain(metrics domain.Metrics) *Metrics {
	return &Metrics{
		Timestamp: timestamppb.New(metrics.Timestamp), CpuUsage: metrics.CPUUsage,
		CpuUser: metrics.CPUUser, CpuSystem: metrics.CPUSystem, CpuIowait: metrics.CPUIowait,
		MemoryUsedBytes: metrics.MemoryUsedBytes, MemoryTotalBytes: metrics.MemoryTotalBytes,
		MemoryUsage: metrics.MemoryUsage, SwapUsedBytes: metrics.SwapUsedBytes,
		DiskUsedBytes: metrics.DiskUsedBytes, DiskTotalBytes: metrics.DiskTotalBytes,
		DiskUsage: metrics.DiskUsage, NetworkRxBps: metrics.NetworkRxBps,
		NetworkTxBps: metrics.NetworkTxBps, Load1: metrics.Load1, Load5: metrics.Load5,
		Load15: metrics.Load15, Processes: int32(metrics.Processes),
		TcpConnections: int32(metrics.TCPConnections), UptimeSeconds: metrics.UptimeSeconds,
	}
}

func MetricsToDomain(metrics *Metrics) domain.Metrics {
	if metrics == nil {
		return domain.Metrics{}
	}
	return domain.Metrics{
		Timestamp: TimestampTime(metrics.Timestamp), CPUUsage: metrics.CpuUsage,
		CPUUser: metrics.CpuUser, CPUSystem: metrics.CpuSystem, CPUIowait: metrics.CpuIowait,
		MemoryUsedBytes: metrics.MemoryUsedBytes, MemoryTotalBytes: metrics.MemoryTotalBytes,
		MemoryUsage: metrics.MemoryUsage, SwapUsedBytes: metrics.SwapUsedBytes,
		DiskUsedBytes: metrics.DiskUsedBytes, DiskTotalBytes: metrics.DiskTotalBytes,
		DiskUsage: metrics.DiskUsage, NetworkRxBps: metrics.NetworkRxBps,
		NetworkTxBps: metrics.NetworkTxBps, Load1: metrics.Load1, Load5: metrics.Load5,
		Load15: metrics.Load15, Processes: int(metrics.Processes),
		TCPConnections: int(metrics.TcpConnections), UptimeSeconds: metrics.UptimeSeconds,
	}
}

func TaskDispatchFromDomain(task domain.AgentTask) *TaskDispatch {
	return &TaskDispatch{
		RunId: task.ID, TaskId: task.TaskID, Kind: task.Kind, Name: task.Name,
		Script: task.Script, ConfigJson: string(task.Config), TimeoutSeconds: int32(task.TimeoutSeconds),
		ScheduledAt: timestamppb.New(task.CreatedAt),
	}
}

func TimestampTime(value *timestamppb.Timestamp) time.Time {
	if value == nil || !value.IsValid() {
		return time.Time{}
	}
	return value.AsTime().UTC()
}
