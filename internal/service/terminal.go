package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/sshclient"
	"github.com/beat/backend/internal/store"
)

const terminalBatchWorkers = 5

var (
	ErrTerminalNodeNotFound          = errors.New("node not found")
	ErrTerminalKeyNotAssigned        = errors.New("ssh key is not assigned")
	ErrTerminalManagedKeyNotFound    = errors.New("managed ssh key not found")
	ErrTerminalPrivateKeyUnavailable = errors.New("managed ssh key has no private key")
)

type TerminalConnector interface {
	OpenTerminal(context.Context, sshclient.Request, io.ReadWriteCloser) error
	Execute(context.Context, sshclient.Request, string) (string, error)
}

type TerminalService struct {
	nodes     *store.NodeStore
	keys      *store.SSHKeyStore
	connector TerminalConnector

	mu       sync.Mutex
	sessions map[*terminalSession]struct{}
}

// terminalSession 标识一条活跃的终端会话；取消其 ctx 即可终止桥接并关闭流。
type terminalSession struct {
	nodeID string
	cancel context.CancelFunc
	stream io.ReadWriteCloser
}

type BatchResult struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

func NewTerminalService(
	nodes *store.NodeStore,
	keys *store.SSHKeyStore,
	connector TerminalConnector,
) *TerminalService {
	return &TerminalService{nodes: nodes, keys: keys, connector: connector,
		sessions: make(map[*terminalSession]struct{})}
}

// ActiveSessions 返回当前活跃终端会话数，供可观测性暴露。
func (s *TerminalService) ActiveSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// CloseAll 终止全部活跃终端会话：取消桥接上下文并关闭流。须在存储关闭前
// 调用（优雅停机路径），确保被劫持的终端连接不会读取已关闭的存储。
func (s *TerminalService) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for session := range s.sessions {
		session.cancel()
		_ = session.stream.Close()
	}
}

func (s *TerminalService) register(session *terminalSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session] = struct{}{}
}

func (s *TerminalService) unregister(session *terminalSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, session)
}

func (s *TerminalService) OpenTerminal(
	ctx context.Context,
	nodeID string,
	stream io.ReadWriteCloser,
) error {
	node, privateKey, err := s.credentials(ctx, nodeID)
	if err != nil {
		return err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &terminalSession{nodeID: nodeID, cancel: cancel, stream: stream}
	s.register(session)
	defer func() {
		cancel()
		s.unregister(session)
		_ = stream.Close()
	}()

	request := sshclient.Request{Node: *node, PrivateKey: privateKey}
	if err := s.connector.OpenTerminal(sessionCtx, request, stream); err != nil {
		if sessionCtx.Err() == nil {
			reportTerminalFailure(stream, err)
		}
		return fmt.Errorf("open ssh terminal: %w", err)
	}
	return nil
}

// reportTerminalFailure 在关闭流之前把失败原因写给终端客户端：桥接层随后就会关闭流，
// 晚于这一步的写入不会被送达。会话被撤销（上下文结束）时不上报，避免把取消说成故障。
func reportTerminalFailure(stream io.ReadWriteCloser, err error) {
	if stream == nil || err == nil {
		return
	}
	_, _ = fmt.Fprintf(stream, "\r\n[terminal error] %s\r\n", err)
}

func (s *TerminalService) ExecuteBatch(
	ctx context.Context,
	nodeIDs []string,
	command string,
) []BatchResult {
	results := make([]BatchResult, len(nodeIDs))
	jobs := make(chan int, len(nodeIDs))
	for index := range nodeIDs {
		jobs <- index
	}
	close(jobs)

	workerCount := min(terminalBatchWorkers, len(nodeIDs))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Go(func() {
			for index := range jobs {
				results[index] = s.execute(ctx, nodeIDs[index], command)
			}
		})
	}
	workers.Wait()
	return results
}

func (s *TerminalService) execute(ctx context.Context, nodeID, command string) BatchResult {
	result := BatchResult{NodeID: nodeID}
	node, privateKey, err := s.credentials(ctx, nodeID)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.NodeName = node.Name
	request := sshclient.Request{Node: *node, PrivateKey: privateKey}
	result.Output, err = s.connector.Execute(ctx, request, command)
	if err != nil {
		result.Output = ""
		result.Error = err.Error()
	}
	return result
}

func (s *TerminalService) credentials(ctx context.Context, nodeID string) (*model.Node, string, error) {
	node, err := s.nodes.GetNode(ctx, nodeID)
	if err != nil {
		return nil, "", fmt.Errorf("get node: %w", err)
	}
	if node == nil {
		return nil, "", ErrTerminalNodeNotFound
	}
	if node.SSHPublicKey == "" {
		return nil, "", ErrTerminalKeyNotAssigned
	}

	key, err := s.keys.GetSSHKeyByPublicKey(ctx, node.SSHPublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("get managed ssh key: %w", err)
	}
	if key == nil {
		return nil, "", ErrTerminalManagedKeyNotFound
	}
	privateKey, err := s.keys.ReadPrivateKey(ctx, key.ID)
	if err != nil {
		return nil, "", fmt.Errorf("read managed ssh key: %w", err)
	}
	if privateKey == "" {
		return nil, "", ErrTerminalPrivateKeyUnavailable
	}
	return node, privateKey, nil
}
