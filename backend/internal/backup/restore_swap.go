package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func (applier *Applier) activate(
	ctx context.Context,
	prepared *preparedRestore,
	token string,
) (map[string]string, error) {
	oldTargets := append([]restoreTarget(nil), prepared.targets...)
	oldTargets = append(oldTargets, restoreTarget{live: prepared.mtsLive})
	rollbacks, movedOld, err := applier.preserveOld(oldTargets, token)
	if err != nil {
		return nil, err
	}
	movedNew, err := applier.activateTargets(prepared.targets)
	if err != nil {
		return nil, errors.Join(err, applier.undoSwap(movedNew, movedOld, rollbacks))
	}
	if err := importMetrics(ctx, prepared.metricsPath, prepared.mtsLive); err != nil {
		_ = os.RemoveAll(prepared.mtsLive)
		return nil, errors.Join(err, applier.undoSwap(movedNew, movedOld, rollbacks))
	}
	return rollbacks, nil
}

func (applier *Applier) swap(prepared *preparedRestore, token string) (map[string]string, error) {
	rollbacks, movedOld, err := applier.preserveOld(prepared.targets, token)
	if err != nil {
		return nil, err
	}
	movedNew, err := applier.activateTargets(prepared.targets)
	if err != nil {
		return nil, errors.Join(err, applier.undoSwap(movedNew, movedOld, rollbacks))
	}
	return rollbacks, nil
}

func (applier *Applier) preserveOld(
	targets []restoreTarget,
	token string,
) (map[string]string, []restoreTarget, error) {
	rollbacks := map[string]string{}
	moved := []restoreTarget{}
	for _, target := range targets {
		if _, err := os.Lstat(target.live); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, nil, fmt.Errorf("inspect restore target %s: %w", target.live, err)
		}
		rollback := target.live + ".restore-rollback-" + token
		if err := applier.rename(target.live, rollback); err != nil {
			return nil, nil, errors.Join(fmt.Errorf("preserve restore target %s: %w", target.live, err),
				applier.restoreOld(moved, rollbacks))
		}
		rollbacks[target.live] = rollback
		moved = append(moved, target)
	}
	return rollbacks, moved, nil
}

func (applier *Applier) activateTargets(targets []restoreTarget) ([]restoreTarget, error) {
	moved := []restoreTarget{}
	for _, target := range targets {
		if err := applier.rename(target.new, target.live); err != nil {
			return moved, fmt.Errorf("activate restore target %s: %w", target.live, err)
		}
		moved = append(moved, target)
	}
	return moved, nil
}

func (applier *Applier) undoSwap(
	movedNew []restoreTarget,
	movedOld []restoreTarget,
	rollbacks map[string]string,
) error {
	var result error
	for index := len(movedNew) - 1; index >= 0; index-- {
		target := movedNew[index]
		result = errors.Join(result, applier.rename(target.live, target.new))
	}
	return errors.Join(result, applier.restoreOld(movedOld, rollbacks))
}

func (applier *Applier) restoreOld(targets []restoreTarget, rollbacks map[string]string) error {
	var result error
	for index := len(targets) - 1; index >= 0; index-- {
		target := targets[index]
		result = errors.Join(result, applier.rename(rollbacks[target.live], target.live))
	}
	return result
}

func (applier *Applier) rollbackApplied(prepared *preparedRestore, rollbacks map[string]string) error {
	removeErr := os.RemoveAll(prepared.mtsLive)
	oldTargets := make([]restoreTarget, 0, len(rollbacks))
	for _, target := range prepared.targets {
		if rollbacks[target.live] != "" {
			oldTargets = append(oldTargets, target)
		}
	}
	if rollbacks[prepared.mtsLive] != "" {
		oldTargets = append(oldTargets, restoreTarget{live: prepared.mtsLive})
	}
	return errors.Join(removeErr, applier.undoSwap(prepared.targets, oldTargets, rollbacks))
}
