package agentupdate

import "context"

type restartObserver interface {
	AutomaticRestarts(context.Context) (int, error)
}

// systemd自动重启可能发生在第一个ACK之前，因此不能仅靠健康PID变化计数。
func (u *Updater) observeManagerRestarts(ctx context.Context, value *journal) (bool, error) {
	manager, ok := u.config.Manager.(restartObserver)
	if !ok {
		return false, nil
	}
	count, err := manager.AutomaticRestarts(ctx)
	if err != nil {
		return false, ErrManager
	}
	if count < 0 || count >= maximumLocalFailures {
		return false, ErrManager
	}
	total := count + 1
	if total <= value.Restarts {
		return false, nil
	}
	value.Restarts = total
	return true, nil
}
