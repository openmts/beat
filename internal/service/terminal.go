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
	return &TerminalService{nodes: nodes, keys: keys, connector: connector}
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
	request := sshclient.Request{Node: *node, PrivateKey: privateKey}
	if err := s.connector.OpenTerminal(ctx, request, stream); err != nil {
		return fmt.Errorf("open ssh terminal: %w", err)
	}
	return nil
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
	if key.PrivateKey == "" {
		return nil, "", ErrTerminalPrivateKeyUnavailable
	}
	return node, key.PrivateKey, nil
}
