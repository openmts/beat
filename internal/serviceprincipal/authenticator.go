package serviceprincipal

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

var (
	// ErrUnauthorized 是所有令牌认证失败的统一对外形态。
	ErrUnauthorized = errors.New("service token is not authorized")
	// ErrUnavailable 表示凭据存储不可用，调用方应返回 503 而不是 401。
	ErrUnavailable = errors.New("service token authentication is unavailable")
)

const defaultTouchInterval = 5 * time.Minute

// PrincipalLookup 是认证所需的最小查询接口。
type PrincipalLookup interface {
	ByHash(ctx context.Context, hash []byte) (*model.ServicePrincipal, error)
	RecordUse(ctx context.Context, id string, at time.Time) error
}

// Authenticator 校验 Bearer 服务令牌并返回已认证主体。
type Authenticator struct {
	lookup PrincipalLookup
	now    func() time.Time
	touch  time.Duration
}

// NewAuthenticator 创建令牌认证器。
func NewAuthenticator(lookup PrincipalLookup) *Authenticator {
	return &Authenticator{lookup: lookup, now: func() time.Time { return time.Now().UTC() }, touch: defaultTouchInterval}
}

// Authenticate 解析固定格式前缀、按摘要加载主体并校验时间与来源策略。
// 未知、过期、撤销、轮换失效与来源不匹配都返回同一个 ErrUnauthorized。
func (authenticator *Authenticator) Authenticate(ctx context.Context, token string, source netip.Addr) (*model.ServicePrincipal, error) {
	if _, ok := DisplayPrefix(token); !ok {
		return nil, ErrUnauthorized
	}
	digest := Digest(token)
	principal, err := authenticator.lookup.ByHash(ctx, digest[:])
	if err != nil {
		if errors.Is(err, store.ErrServicePrincipalNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, ErrUnavailable
	}
	now := authenticator.now()
	if !Matches(principal.TokenHash, token) || !Usable(*principal, now) || !AllowsSource(*principal, source) {
		return nil, ErrUnauthorized
	}
	authenticator.recordUse(ctx, principal, now)
	return principal, nil
}

// recordUse 只做有界节流的尽力更新，失败不影响已完成认证。
func (authenticator *Authenticator) recordUse(ctx context.Context, principal *model.ServicePrincipal, now time.Time) {
	if principal.LastUsedAt != nil && now.Sub(*principal.LastUsedAt) < authenticator.touch {
		return
	}
	_ = authenticator.lookup.RecordUse(ctx, principal.ID, now)
}
