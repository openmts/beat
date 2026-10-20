package backup

import "context"

// SecretLifecycle 保证备份快照和归档根密钥处于同一轮换代际。
type SecretLifecycle interface {
	AcquireBackup(context.Context) (func(), error)
}

func (service *Service) SetSecretLifecycle(lifecycle SecretLifecycle) {
	service.secretLifecycle = lifecycle
}

func (service *Service) acquireSecretLifecycle(ctx context.Context) (func(), error) {
	if service.secretLifecycle == nil {
		return func() {}, nil
	}
	return service.secretLifecycle.AcquireBackup(ctx)
}
