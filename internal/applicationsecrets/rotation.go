package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math"

	"github.com/google/uuid"
)

type Status struct {
	Healthy          bool   `json:"healthy"`
	RootAvailable    bool   `json:"root_available"`
	RegistryValid    bool   `json:"registry_valid"`
	BackfillComplete bool   `json:"backfill_complete"`
	Rotation         string `json:"rotation"`
	Generation       int64  `json:"generation"`
	DataKeys         int    `json:"data_keys"`
	RotationID       string `json:"rotation_id,omitempty"`
}

func (service *Service) Status() Status {
	service.gate.RLock()
	defer service.gate.RUnlock()
	status := Status{RootAvailable: service.root != nil, RegistryValid: service.ring != nil,
		Generation: service.state.Generation, Rotation: "idle", RotationID: service.state.RotationID}
	status.BackfillComplete = service.state.Phase == "ready" || service.state.Phase == "rotating"
	status.Healthy = status.RootAvailable && status.RegistryValid && status.BackfillComplete && !service.unhealthy.Load()
	if service.ring != nil {
		status.DataKeys = len(service.ring.ciphers)
	}
	if service.state.Phase == "rotating" {
		status.Rotation = "running"
	}
	if service.unhealthy.Load() {
		status.Rotation = "degraded"
	}
	return status
}

// StartRotation 仅供可信内部调用；HTTP 必须使用带事务授权与审计的 StartOwnerRotation。
func (service *Service) StartRotation(ctx context.Context) (Status, error) {
	return service.startRotation(ctx, nil)
}

func (service *Service) StartOwnerRotation(ctx context.Context, actor RotationActor) (Status, error) {
	return service.startRotation(ctx, &actor)
}

func (service *Service) startRotation(ctx context.Context, actor *RotationActor) (Status, error) {
	service.workerMu.Lock()
	defer service.workerMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	if !service.lifecycle.TryLock() {
		return Status{}, ErrLifecycleBusy
	}
	if err := service.activateAuthorizedRotation(ctx, actor); err != nil {
		if errors.Is(err, ErrUnhealthy) || errors.Is(err, ErrCiphertext) {
			service.unhealthy.Store(true)
		}
		service.lifecycle.Unlock()
		return Status{}, err
	}
	service.wg.Add(1)
	go service.runRotation()
	return service.Status(), nil
}

func (service *Service) activateRotation(ctx context.Context) error {
	return service.activateAuthorizedRotation(ctx, nil)
}

func (service *Service) activateAuthorizedRotation(ctx context.Context, actor *RotationActor) error {
	service.gate.Lock()
	defer service.gate.Unlock()
	if service.unhealthy.Load() || service.state.Phase != "ready" || service.ctx.Err() != nil {
		return ErrUnhealthy
	}
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = service.authorizeRotation(ctx, tx, actor)
	if err != nil {
		return err
	}
	state, err := readState(ctx, tx)
	if err != nil {
		return err
	}
	if state.Generation == math.MaxInt64 || state.Phase != "ready" {
		return ErrUnhealthy
	}
	rotationID, err := service.activateDataKey(ctx, tx, state)
	if err != nil {
		return err
	}
	if err := service.commitRotation(ctx, tx, rotationCommit{actor: actor, id: rotationID, generation: state.Generation + 1}); err != nil {
		return err
	}
	if err := service.reload(ctx); err != nil {
		service.unhealthy.Store(true)
		return err
	}
	return nil
}

func (service *Service) activateDataKey(ctx context.Context, tx *sql.Tx, state lifecycleState) (string, error) {
	if err := service.retireKeys(ctx, tx); err != nil {
		return "", err
	}
	operation, err := uuid.NewRandomFromReader(service.random)
	if err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, "UPDATE application_data_keys SET state = 'decrypt_only' WHERE id = ? AND state = 'active'", state.ActiveID)
	if err := requireOne(result, err); err != nil {
		return "", err
	}
	id, err := service.insertKey(ctx, tx, state.Generation+1)
	if err != nil {
		return "", err
	}
	result, err = tx.ExecContext(ctx, `UPDATE application_secret_state SET phase = 'rotating', generation = ?,
		active_key_id = ?, cursor_source = 0, cursor_id = '', rotation_id = ?, updated_at = ? WHERE id = 1`, state.Generation+1, id, operation.String(), service.now().Unix())
	return operation.String(), requireOne(result, err)
}

func (service *Service) retireKeys(ctx context.Context, tx *sql.Tx) error {
	references, err := validateResources(ctx, tx, validationConfig{ring: service.ring, requireActive: true})
	if err != nil {
		return err
	}
	keys, err := readRegistry(ctx, tx)
	if err != nil {
		return err
	}
	remaining := len(keys)
	for _, key := range keys {
		if key.State != "decrypt_only" || !key.RetireAfter.Valid || key.RetireAfter.Int64 > service.now().Unix() {
			continue
		}
		if references[key.ID] != 0 {
			// requireActive 已验证 SQLite 资源；仅在途 spool 帧可保留旧数据密钥。
			continue
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM application_data_keys WHERE id = ? AND state = 'decrypt_only'", key.ID)
		if err := requireOne(result, err); err != nil {
			return err
		}
		remaining--
	}
	if remaining >= maximumDataKeys {
		return ErrKeyLimit
	}
	return nil
}

func (service *Service) runRotation() {
	defer service.wg.Done()
	defer service.lifecycle.Unlock()
	err := service.checkpoint("rotation_activated")
	if err == nil {
		err = service.resume(service.ctx)
	}
	if err != nil {
		service.unhealthy.Store(true)
		status := service.Status()
		slog.Error("application secret rotation interrupted", "reason", "rotation_incomplete", "rotation_id", status.RotationID, "generation", status.Generation)
	}
}
