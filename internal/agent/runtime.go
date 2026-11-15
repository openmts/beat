package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"beat/internal/domain"
	"beat/internal/networkprobe"
	beatv1 "beat/internal/protocol/beatv1"
	"beat/internal/version"
)

type RuntimeConfig struct {
	ServerURL          string
	Token              string
	AgentID            string
	StateFile          string
	NodeName           string
	Labels             map[string]string
	Interval           time.Duration
	DiskPath           string
	Logger             *slog.Logger
	InsecureSkipVerify bool
}

type Runtime struct {
	cfg       RuntimeConfig
	collector *Collector
	client    *Client

	mu        sync.Mutex
	active    map[string]struct{}
	completed map[string]*beatv1.TaskResult
	taskQueue chan taskJob
	uninstall bool
	upgrading bool
	restart   bool
}

type taskJob struct {
	ctx    context.Context
	cancel context.CancelFunc
	task   *beatv1.TaskDispatch
}

func NewRuntime(cfg RuntimeConfig) (*Runtime, error) {
	if strings.TrimSpace(cfg.AgentID) == "" {
		return nil, errors.New("Agent ID 不能为空")
	}
	if cfg.Interval < 5*time.Second {
		cfg.Interval = 15 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	client, err := NewClient(cfg.ServerURL, cfg.Token, cfg.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		cfg: cfg, collector: NewCollector(cfg.DiskPath), client: client,
		active: map[string]struct{}{}, completed: map[string]*beatv1.TaskResult{},
		taskQueue: make(chan taskJob, 128),
	}, nil
}

func (r *Runtime) Run(ctx context.Context) error {
	defer r.client.Close()
	for worker := 0; worker < 2; worker++ {
		go r.taskWorker()
	}
	backoff := time.Second
	for {
		err := r.runSession(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrUpgradeRestart) || r.restartRequested() {
			return ErrUpgradeRestart
		}
		if err == nil || r.uninstallRequested() {
			return nil
		}
		r.cfg.Logger.Warn("agent_grpc_session_failed", "server", r.cfg.ServerURL, "error", err, "retry_in", backoff.String())
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (r *Runtime) runSession(ctx context.Context) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	hostname := r.cfg.NodeName
	if hostname == "" {
		hostname = r.cfg.AgentID
	}
	welcome, err := r.client.Open(sessionCtx, &beatv1.Hello{AgentId: r.cfg.AgentID, Hostname: hostname, Version: version.Version})
	if err != nil {
		return err
	}
	r.cfg.Logger.Info("agent_grpc_connected", "server", r.cfg.ServerURL, "node_id", welcome.GetNodeId(), "server_version", welcome.GetServerVersion())

	receiveDone := make(chan error, 1)
	go func() { receiveDone <- r.receiveLoop(sessionCtx, cancel) }()

	reportNow := func() {
		snapshot, err := r.collector.Collect()
		if err != nil {
			r.cfg.Logger.Warn("collect_failed", "error", err)
			return
		}
		nodeName := r.cfg.NodeName
		if nodeName == "" {
			nodeName = snapshot.Hostname
		}
		report := domain.Report{
			AgentID: r.cfg.AgentID, Hostname: nodeName, Address: snapshot.Address,
			OS: snapshot.OS, Kernel: snapshot.Kernel, Arch: snapshot.Arch,
			AgentVersion: version.Version, IntervalSeconds: int(r.cfg.Interval.Seconds()),
			Labels: r.cfg.Labels, Interfaces: snapshot.Interfaces, CPUCores: snapshot.CPUCores,
			CPUModel: snapshot.CPUModel, Metrics: snapshot.Metrics,
		}
		if err := r.client.SendReport(sessionCtx, beatv1.ReportFromDomain(report)); err != nil {
			r.cfg.Logger.Warn("report_failed", "error", err)
			cancel()
			return
		}
		r.cfg.Logger.Info("report_ok", "node", nodeName, "cpu", snapshot.Metrics.CPUUsage, "memory", snapshot.Metrics.MemoryUsage)
	}

	reportNow()
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-sessionCtx.Done():
			if r.uninstallRequested() {
				return nil
			}
			if r.restartRequested() {
				return ErrUpgradeRestart
			}
			return sessionCtx.Err()
		case err := <-receiveDone:
			if err == nil {
				return nil
			}
			return err
		case <-ticker.C:
			reportNow()
		}
	}
}

func (r *Runtime) receiveLoop(ctx context.Context, cancel context.CancelFunc) error {
	for {
		message, err := r.client.Receive()
		if err != nil {
			return err
		}
		switch payload := message.Payload.(type) {
		case *beatv1.ServerMessage_Welcome:
			continue
		case *beatv1.ServerMessage_Heartbeat:
			continue
		case *beatv1.ServerMessage_TaskResultAck:
			ack := payload.TaskResultAck
			if ack.GetAccepted() {
				r.cfg.Logger.Info("task_result_acked", "run_id", ack.GetRunId())
			} else {
				r.cfg.Logger.Warn("task_result_rejected", "run_id", ack.GetRunId(), "message", ack.GetMessage())
			}
		case *beatv1.ServerMessage_TaskDispatch:
			r.dispatchTask(ctx, cancel, payload.TaskDispatch)
		case *beatv1.ServerMessage_AgentUpgrade:
			go r.handleUpgrade(ctx, cancel, payload.AgentUpgrade)
		}
	}
}

func (r *Runtime) dispatchTask(ctx context.Context, cancel context.CancelFunc, task *beatv1.TaskDispatch) {
	if task.GetRunId() == "" {
		return
	}
	r.mu.Lock()
	if result, exists := r.completed[task.GetRunId()]; exists {
		r.mu.Unlock()
		_ = r.client.SendTaskResult(ctx, result)
		return
	}
	if _, exists := r.active[task.GetRunId()]; exists {
		r.mu.Unlock()
		return
	}
	r.active[task.GetRunId()] = struct{}{}
	r.mu.Unlock()
	select {
	case r.taskQueue <- taskJob{ctx: ctx, cancel: cancel, task: task}:
	default:
		r.mu.Lock()
		delete(r.active, task.GetRunId())
		result := &beatv1.TaskResult{RunId: task.GetRunId(), TaskId: task.GetTaskId(), Status: "failed", ExitCode: -1, Output: "Agent 任务队列已满，请稍后重试"}
		r.completed[task.GetRunId()] = result
		r.mu.Unlock()
		_ = r.client.SendTaskResult(ctx, result)
	}
}

func (r *Runtime) taskWorker() {
	for job := range r.taskQueue {
		r.executeTask(job.ctx, job.cancel, job.task)
	}
}

func (r *Runtime) executeTask(ctx context.Context, cancel context.CancelFunc, task *beatv1.TaskDispatch) {
	defer func() {
		r.mu.Lock()
		delete(r.active, task.GetRunId())
		r.mu.Unlock()
	}()
	r.cfg.Logger.Info("task_execution_started", "run_id", task.GetRunId(), "task_id", task.GetTaskId(), "kind", task.GetKind(), "name", task.GetName())
	result := &beatv1.TaskResult{RunId: task.GetRunId(), TaskId: task.GetTaskId(), Status: "failed", ExitCode: -1}
	switch task.GetKind() {
	case "uninstall_agent":
		message, err := ExecuteUninstall(r.cfg.StateFile)
		result.Output = message
		if err != nil {
			result.Output = err.Error()
		} else {
			result.Status = "success"
			result.ExitCode = 0
		}
	case "shell_script":
		timeout := time.Duration(task.GetTimeoutSeconds()) * time.Second
		execution := ExecuteScript(ctx, task.GetScript(), timeout)
		result.Status = execution.Status
		result.Output = execution.Output
		result.ExitCode = int32(execution.ExitCode)
		result.DurationMs = execution.DurationMs
	case networkprobe.Kind:
		config, err := networkprobe.ParseConfig([]byte(task.GetConfigJson()))
		if err != nil {
			result.Output = err.Error()
			break
		}
		timeout := time.Duration(task.GetTimeoutSeconds()) * time.Second
		probeCtx, cancelProbe := context.WithTimeout(ctx, timeout)
		probeResult := networkprobe.RunTCP(probeCtx, config)
		cancelProbe()
		output, err := json.Marshal(probeResult)
		if err != nil {
			result.Output = "序列化探测结果失败: " + err.Error()
			break
		}
		result.Status = "success"
		result.ExitCode = 0
		result.DurationMs = probeResult.DurationMs
		result.Output = string(output)
	default:
		result.Output = "未知任务类型: " + task.GetKind()
	}
	r.mu.Lock()
	r.completed[task.GetRunId()] = result
	r.mu.Unlock()
	if err := r.client.SendTaskResult(ctx, result); err != nil {
		r.cfg.Logger.Warn("task_result_send_failed", "run_id", task.GetRunId(), "error", err)
		cancel()
		return
	}
	r.cfg.Logger.Info("task_finished", "run_id", task.GetRunId(), "task_id", task.GetTaskId(), "status", result.Status, "exit_code", result.ExitCode, "duration_ms", result.DurationMs)
	if task.GetKind() == "uninstall_agent" && result.Status == "success" {
		r.mu.Lock()
		r.uninstall = true
		r.mu.Unlock()
		time.Sleep(300 * time.Millisecond)
		cancel()
	}
}

func (r *Runtime) uninstallRequested() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.uninstall
}
