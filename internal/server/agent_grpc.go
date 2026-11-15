package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"beat/internal/domain"
	beatv1 "beat/internal/protocol/beatv1"
	"beat/internal/version"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errAgentUninstalled = errors.New("agent has been uninstalled")

const agentSessionQueueSize = 128

type agentControlService struct {
	beatv1.UnimplementedAgentControlServer
	server *Server
}

func (s *agentControlService) Connect(stream beatv1.AgentControl_ConnectServer) error {
	token := grpcAgentToken(stream.Context())
	if token == "" {
		return status.Error(codes.Unauthenticated, "缺少 Agent 凭据")
	}
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "读取 Agent 握手失败: %v", err)
	}
	hello := first.GetHello()
	if hello == nil || strings.TrimSpace(hello.AgentId) == "" || strings.TrimSpace(hello.Hostname) == "" {
		return status.Error(codes.InvalidArgument, "首条消息必须包含 agentId 和 hostname")
	}
	now := time.Now().UTC()
	authorized, err := s.server.authorizeAgent(stream.Context(), token, hello.AgentId, now)
	if err != nil {
		return status.Errorf(codes.Internal, "Agent 凭据校验失败: %v", err)
	}
	if !authorized {
		return status.Error(codes.Unauthenticated, "Agent 凭据无效或注册令牌已过期")
	}
	deleted, err := s.server.store.IsAgentDeleted(stream.Context(), hello.AgentId)
	if err != nil {
		return status.Errorf(codes.Internal, "Agent 状态校验失败: %v", err)
	}
	if deleted {
		return status.Error(codes.PermissionDenied, "该 Agent 已从 Beat 卸载，请重新生成接入命令")
	}
	s.server.reconcileAgentUpgrade(hello.AgentId, hello.Version)

	session := newAgentSession(hello.AgentId, stream)
	s.server.agentSessions.register(session)
	defer func() {
		s.server.agentSessions.unregister(hello.AgentId, session)
		session.close()
	}()
	session.start()
	session.enqueue(&beatv1.ServerMessage{Payload: &beatv1.ServerMessage_Welcome{Welcome: &beatv1.Welcome{
		ServerTime: timestamppb.New(now), MaxConcurrentTasks: 2,
		HeartbeatIntervalSeconds: 15, ServerVersion: version.Version,
	}}})
	s.server.logger.Info("agent_stream_connected", "agent_id", hello.AgentId, "hostname", hello.Hostname, "version", hello.Version)
	if err := s.server.pushPendingAgentTasks(stream.Context(), hello.AgentId); err != nil {
		s.server.logger.Warn("push_pending_agent_tasks_failed", "agent_id", hello.AgentId, "error", err)
	}
	if active, err := s.server.store.GetActiveAgentUpgradeBatch(stream.Context()); err == nil && active != nil {
		s.server.dispatchAgentUpgrades(stream.Context(), active.ID)
	}

	for {
		message, err := stream.Recv()
		if err != nil {
			if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
				s.server.logger.Info("agent_stream_disconnected", "agent_id", hello.AgentId)
				return nil
			}
			s.server.logger.Info("agent_stream_closed", "agent_id", hello.AgentId, "error", err)
			return nil
		}
		switch payload := message.Payload.(type) {
		case *beatv1.AgentMessage_Hello:
			return status.Error(codes.InvalidArgument, "连接建立后不能重复发送 Hello")
		case *beatv1.AgentMessage_Report:
			if payload.Report.GetAgentId() != hello.AgentId {
				return status.Error(codes.PermissionDenied, "上报数据中的 agentId 与连接身份不一致")
			}
			node, tasks, err := s.server.processAgentReport(stream.Context(), beatv1.ReportToDomain(payload.Report))
			if errors.Is(err, errAgentUninstalled) {
				return status.Error(codes.PermissionDenied, err.Error())
			}
			if err != nil {
				s.server.logger.Error("process_agent_report_failed", "agent_id", hello.AgentId, "error", err)
				return status.Error(codes.Internal, "指标上报处理失败")
			}
			session.setNodeID(node.ID)
			for _, task := range tasks {
				if !session.markTaskSent(task.ID) {
					continue
				}
				if !session.enqueueTask(task) {
					s.server.logger.Warn("agent_task_enqueue_failed", "agent_id", hello.AgentId, "task_id", task.ID)
				}
			}
			if err := s.server.pushPendingAgentTasks(stream.Context(), hello.AgentId); err != nil {
				s.server.logger.Warn("push_pending_agent_tasks_failed", "agent_id", hello.AgentId, "error", err)
			}
		case *beatv1.AgentMessage_TaskResult:
			result := payload.TaskResult
			if strings.TrimSpace(result.GetRunId()) == "" {
				return status.Error(codes.InvalidArgument, "任务结果缺少 runId")
			}
			if err := validateTaskResult(result); err != nil {
				session.enqueueTaskResultAck(result.GetRunId(), false, err.Error())
				continue
			}
			completed, err := s.server.completeAgentTaskResult(stream.Context(), hello.AgentId, result)
			if err != nil {
				s.server.logger.Error("complete_agent_task_result_failed", "agent_id", hello.AgentId, "run_id", result.GetRunId(), "error", err)
				session.enqueueTaskResultAck(result.GetRunId(), false, "任务结果保存失败")
				continue
			}
			if completed {
				s.server.logger.Info("agent_task_result_completed", "agent_id", hello.AgentId, "run_id", result.GetRunId(), "status", result.GetStatus())
			}
			session.enqueueTaskResultAck(result.GetRunId(), completed, taskResultAckMessage(completed))
		case *beatv1.AgentMessage_Heartbeat:
			session.enqueue(&beatv1.ServerMessage{Payload: &beatv1.ServerMessage_Heartbeat{Heartbeat: &beatv1.ServerHeartbeat{SentAt: timestamppb.Now()}}})
		case *beatv1.AgentMessage_UpgradeEvent:
			event := payload.UpgradeEvent
			if strings.TrimSpace(event.GetOperationId()) == "" {
				return status.Error(codes.InvalidArgument, "升级事件缺少 operationId")
			}
			accepted, err := s.server.store.UpdateAgentUpgradeProgress(stream.Context(), hello.AgentId, event.GetOperationId(), event.GetPhase(), event.GetMessage(), time.Now().UTC())
			if err != nil {
				s.server.logger.Warn("agent_upgrade_progress_failed", "agent_id", hello.AgentId, "operation_id", event.GetOperationId(), "error", err)
			} else {
				s.server.logger.Info("agent_upgrade_progress", "agent_id", hello.AgentId, "operation_id", event.GetOperationId(), "phase", event.GetPhase(), "accepted", accepted)
			}
			if event.GetPhase() == "failed" && accepted {
				if active, err := s.server.store.GetActiveAgentUpgradeBatch(stream.Context()); err == nil && active != nil {
					s.server.dispatchAgentUpgrades(stream.Context(), active.ID)
				}
			}
		default:
			return status.Error(codes.InvalidArgument, "未知的 Agent 消息类型")
		}
	}
}

func (s *Server) processAgentReport(ctx context.Context, report domain.Report) (domain.Node, []domain.AgentTask, error) {
	if strings.TrimSpace(report.AgentID) == "" || strings.TrimSpace(report.Hostname) == "" {
		return domain.Node{}, nil, errors.New("agentId 和 hostname 不能为空")
	}
	now := time.Now().UTC()
	deleted, err := s.store.IsAgentDeleted(ctx, report.AgentID)
	if err != nil {
		return domain.Node{}, nil, err
	}
	if deleted {
		return domain.Node{}, nil, errAgentUninstalled
	}
	if report.IntervalSeconds <= 0 {
		report.IntervalSeconds = 15
	}
	if report.Metrics.Timestamp.IsZero() {
		report.Metrics.Timestamp = now
	}
	node, err := s.store.UpsertNode(ctx, report, now)
	if err != nil {
		return domain.Node{}, nil, fmt.Errorf("upsert node: %w", err)
	}
	if err := s.tsdb.Append(ctx, node.ID, report.Metrics); err != nil {
		return domain.Node{}, nil, fmt.Errorf("append metrics: %w", err)
	}
	tasks, err := s.store.ScheduleAgentTaskRuns(ctx, report.AgentID, node.ID, node.Hostname, now)
	if err != nil {
		s.logger.Warn("schedule_agent_task_runs_failed", "agent_id", report.AgentID, "error", err)
	}
	return node, tasks, nil
}

func (s *Server) completeAgentTaskResult(ctx context.Context, agentID string, result *beatv1.TaskResult) (bool, error) {
	now := time.Now().UTC()
	runCompleted, err := s.store.CompleteTaskRun(ctx, result.GetRunId(), agentID, result.GetStatus(), result.GetOutput(), int(result.GetExitCode()), result.GetDurationMs(), now)
	if err != nil {
		return false, err
	}
	if runCompleted {
		if err := s.persistNetworkProbeResult(ctx, result.GetRunId()); err != nil {
			s.logger.Warn("persist_network_probe_result_failed", "run_id", result.GetRunId(), "error", err)
		}
		if err := s.persistShellTaskMetrics(ctx, result.GetRunId()); err != nil {
			s.logger.Warn("persist_shell_task_metrics_failed", "run_id", result.GetRunId(), "error", err)
		}
		return true, nil
	}
	agentStatus := result.GetStatus()
	if agentStatus == "timeout" {
		agentStatus = "failed"
	}
	return s.store.CompleteAgentTask(ctx, result.GetRunId(), agentID, agentStatus, result.GetOutput(), now)
}

func validateTaskResult(result *beatv1.TaskResult) error {
	if result.GetDurationMs() < 0 {
		return errors.New("任务耗时无效")
	}
	switch result.GetStatus() {
	case "success", "failed", "timeout":
		return nil
	default:
		return errors.New("任务状态无效")
	}
}

func taskResultAckMessage(accepted bool) string {
	if accepted {
		return "任务结果已记录"
	}
	return "任务不存在、已结束或状态无效"
}

func grpcAgentToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get("authorization") {
		if token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer ")); token != "" {
			return token
		}
	}
	if values := md.Get("x-beat-token"); len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

type agentSessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*agentSession
}

func newAgentSessionRegistry() *agentSessionRegistry {
	return &agentSessionRegistry{sessions: map[string]*agentSession{}}
}

func (r *agentSessionRegistry) register(session *agentSession) {
	r.mu.Lock()
	previous := r.sessions[session.agentID]
	r.sessions[session.agentID] = session
	r.mu.Unlock()
	if previous != nil && previous != session {
		previous.close()
	}
}

func (r *agentSessionRegistry) unregister(agentID string, session *agentSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions[agentID] == session {
		delete(r.sessions, agentID)
	}
}

func (r *agentSessionRegistry) get(agentID string) *agentSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[agentID]
}

type agentSession struct {
	agentID string
	stream  beatv1.AgentControl_ConnectServer
	outbox  chan *beatv1.ServerMessage
	done    chan struct{}

	mu        sync.Mutex
	nodeID    string
	sentTasks map[string]struct{}
	closeOnce sync.Once
}

func newAgentSession(agentID string, stream beatv1.AgentControl_ConnectServer) *agentSession {
	return &agentSession{
		agentID: agentID, stream: stream, outbox: make(chan *beatv1.ServerMessage, agentSessionQueueSize),
		done: make(chan struct{}), sentTasks: map[string]struct{}{},
	}
}

func (s *agentSession) start() {
	go func() {
		for {
			select {
			case <-s.done:
				return
			case message := <-s.outbox:
				if message == nil {
					continue
				}
				if err := s.stream.Send(message); err != nil {
					s.close()
					return
				}
			}
		}
	}()
}

func (s *agentSession) enqueue(message *beatv1.ServerMessage) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.outbox <- message:
		return true
	case <-s.done:
		return false
	default:
		return false
	}
}

func (s *agentSession) enqueueTask(task domain.AgentTask) bool {
	return s.enqueue(&beatv1.ServerMessage{Payload: &beatv1.ServerMessage_TaskDispatch{TaskDispatch: beatv1.TaskDispatchFromDomain(task)}})
}

func (s *agentSession) enqueueTaskResultAck(runID string, accepted bool, message string) {
	s.enqueue(&beatv1.ServerMessage{Payload: &beatv1.ServerMessage_TaskResultAck{TaskResultAck: &beatv1.TaskResultAck{
		RunId: runID, Accepted: accepted, Message: message,
	}}})
}

func (s *agentSession) enqueueAgentUpgrade(upgrade *beatv1.AgentUpgrade) bool {
	return s.enqueue(&beatv1.ServerMessage{Payload: &beatv1.ServerMessage_AgentUpgrade{AgentUpgrade: upgrade}})
}

func (s *agentSession) markTaskSent(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sentTasks[id]; exists {
		return false
	}
	s.sentTasks[id] = struct{}{}
	return true
}

func (s *agentSession) setNodeID(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeID = nodeID
}

func (s *agentSession) close() {
	s.closeOnce.Do(func() { close(s.done) })
}

func (s *Server) pushPendingAgentTasks(ctx context.Context, agentID string) error {
	session := s.agentSessions.get(agentID)
	if session == nil {
		return nil
	}
	tasks, err := s.store.ClaimAgentTasks(ctx, agentID, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if !session.markTaskSent(task.ID) {
			continue
		}
		if !session.enqueueTask(task) {
			return errors.New("Agent 会话发送队列已满")
		}
	}
	return nil
}
