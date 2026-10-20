package agentinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/beat/backend/internal/agentjoin"
)

const requestTimeout = 20 * time.Second

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type joinExchange struct {
	server string
	token  string
	body   agentjoin.Request
}

func (installer *Installer) exchange(ctx context.Context, exchange joinExchange) (agentjoin.Result, error) {
	body, err := json.Marshal(exchange.body)
	if err != nil {
		return agentjoin.Result{}, fmt.Errorf("encode join request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		exchange.server+"/api/v1/agents/join", bytes.NewReader(body))
	if err != nil {
		return agentjoin.Result{}, fmt.Errorf("create join request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+exchange.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := installer.client.Do(request)
	if err != nil {
		return agentjoin.Result{}, fmt.Errorf("join request failed; rerun the same command to recover: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return agentjoin.Result{}, joinResponseError(response)
	}
	var envelope agentjoin.Envelope[agentjoin.Result]
	if err := decodeJSON(io.LimitReader(response.Body, maxPrivateFile+1), &envelope); err != nil {
		return agentjoin.Result{}, errors.New("invalid join response: expected a bounded JSON envelope")
	}
	if envelope.Code != http.StatusOK {
		return agentjoin.Result{}, errors.New("server rejected join; rerun the same command after resolving the server error")
	}
	return envelope.Data, nil
}

func decodeJSON(reader io.Reader, target any) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if len(data) > maxPrivateFile {
		return errors.New("JSON exceeds the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("JSON must contain exactly one value")
	}
	return nil
}

var errAgentIdentityUnavailable = errors.New("saved agent identity is no longer active; retain the saved files and contact an administrator for recovery")

var errNodeNameConflict = errors.New("node name already exists; rerun the same join command with --name <unique-name>")

func joinResponseError(response *http.Response) error {
	if response.StatusCode == http.StatusUnauthorized {
		return errors.New("join token is invalid or expired; generate a new command on the server and rerun it here to recover the saved identity")
	}
	if response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusForbidden {
		var envelope agentjoin.Envelope[any]
		if err := decodeJSON(io.LimitReader(response.Body, maxPrivateFile+1), &envelope); err == nil && envelope.Code == response.StatusCode {
			if classified := classifiedJoinError(envelope); classified != nil {
				return classified
			}
		}
	}
	return fmt.Errorf("join rejected (HTTP %d); check token validity and server availability", response.StatusCode)
}

func classifiedJoinError(envelope agentjoin.Envelope[any]) error {
	if envelope.Code == http.StatusConflict && envelope.Message == agentjoin.NodeNameConflictMessage {
		return errNodeNameConflict
	}
	if envelope.Code == http.StatusForbidden && envelope.Message == agentjoin.AgentIdentityUnavailableMessage {
		return errAgentIdentityUnavailable
	}
	return nil
}
