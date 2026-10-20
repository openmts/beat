package adminauth

import (
	"context"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/loginsecurity"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// loginLocked 检查持久化阈值是否仍处于锁定窗口，使重启后锁定期依然有效。
func (service *Service) loginLocked(ctx context.Context, request LoginRequest, now time.Time) (bool, error) {
	credentials, err := service.config.Store.SecurityCredentials(ctx)
	if err != nil {
		return false, err
	}
	keys := service.loginThresholdKeys(credentials, request.Username, request.IPAddress)
	return service.config.Store.SecurityLockActive(ctx, keys, now)
}

// clearLoginThresholds 在成功登录后清理该来源与用户名的失败窗口。
func (service *Service) clearLoginThresholds(ctx context.Context, username, ipAddress string, now time.Time) {
	credentials, err := service.config.Store.SecurityCredentials(ctx)
	if err != nil {
		slog.Warn("read security credentials", "error", err)
		return
	}
	if err := service.config.Store.ClearSecurityThresholds(ctx,
		service.loginThresholdKeys(credentials, username, ipAddress)); err != nil {
		slog.Warn("clear security thresholds", "error", err)
	}
}

func (service *Service) loginThresholdKeys(
	credentials store.SecurityCredentials, username, ipAddress string,
) []store.LoginThresholdKey {
	source := loginsecurity.SummarizeSource(ipAddress)
	return []store.LoginThresholdKey{
		{KeyKind: store.SecurityFingerprintKindSource,
			KeyHash: loginsecurity.Fingerprint(credentials.FingerprintKey, credentials.FingerprintEpoch, "source:"+source.Prefix)},
		{KeyKind: "username",
			KeyHash: loginsecurity.Fingerprint(credentials.FingerprintKey, credentials.FingerprintEpoch,
				"password:"+model.NormalizeUsername(username))},
	}
}

// failureReason 把验证错误映射为固定原因代码，未知错误不产生可疑规则。
func failureReason(err error) string {
	if err == ErrInvalidTOTP {
		return model.SecurityReasonInvalidTOTP
	}
	return ""
}
