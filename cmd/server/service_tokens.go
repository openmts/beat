package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/serviceprincipal"
	"github.com/beat/backend/internal/serviceprincipalhttp"
	"github.com/beat/backend/internal/store"
)

var errServicePrincipalDependencies = errors.New("service token dependencies are unavailable")

func (services *serverServices) initializeServicePrincipals(ctx context.Context, stores *serverStores) error {
	if services == nil || stores == nil || stores.sqlite == nil || stores.sqlite.DB == nil {
		return errServicePrincipalDependencies
	}
	principalStore := store.NewServicePrincipalStore(stores.sqlite.DB)
	services.servicePrincipalStore = principalStore
	service := serviceprincipal.NewService(principalStore)
	if revoked, err := service.Expire(ctx, time.Now().UTC()); err != nil {
		slog.Error("service token expiry failed", "error", err)
	} else if revoked > 0 {
		slog.Info("expired service tokens revoked", "count", revoked)
	}
	services.servicePrincipals = serviceprincipalhttp.New(service)
	services.serviceTokenAuthenticator = serviceprincipal.NewAuthenticator(principalStore)
	services.serviceTokenLimiter = serviceprincipal.NewLimiter(serviceprincipal.DefaultRateLimits())
	services.serviceTokenFailureLimiter = serviceprincipal.NewLimiter(failedServiceTokenRateLimits())
	return nil
}

// registerServiceTokenMetrics 把服务令牌计数接到 Prometheus 抓取；未初始化时保持空快照。
func registerServiceTokenMetrics(metrics *observability.Registry, services *serverServices) {
	if metrics == nil || services == nil || services.servicePrincipalStore == nil {
		return
	}
	principals := services.servicePrincipalStore
	metrics.SetServiceTokenStatistics(func() (observability.ServiceTokenStatistics, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		statistics, err := principals.Statistics(ctx, time.Now().UTC())
		if err != nil {
			return observability.ServiceTokenStatistics{}, err
		}
		return observability.ServiceTokenStatistics{
			Active: uint64(statistics.Active), Expiring: uint64(statistics.Expiring), Revoked: uint64(statistics.Revoked),
		}, nil
	})
}

// failedServiceTokenSourceBurst 是单来源每秒允许的失败认证尝试数；通过该准入的尝试才写审计。
const failedServiceTokenSourceBurst = 30

// failedServiceTokenRateLimits 收紧失败认证的来源预算，防止未知令牌探测放大审计写入。
func failedServiceTokenRateLimits() serviceprincipal.RateLimits {
	limits := serviceprincipal.DefaultRateLimits()
	limits.Source = serviceprincipal.RateLimit{Burst: failedServiceTokenSourceBurst, Interval: time.Second}
	return limits
}

// nodeVisibility 是可见性判定所需的最小节点查询。
type nodeVisibility interface {
	GetPublicNode(ctx context.Context, id string) (*model.Node, error)
}

// nodeResourceVisibility 把节点公开性映射为 public 可见性令牌的判定；查询失败按依赖
// 不可用上抛，由中间件返回 503，而不是把私有数据降级放行。
func nodeResourceVisibility(nodes nodeVisibility) middleware.ServiceResourceVisibility {
	return func(ctx context.Context, kind model.ServiceResourceKind, id string) (bool, error) {
		if kind != model.ServiceResourceNode || id == "" || nodes == nil {
			return false, nil
		}
		node, err := nodes.GetPublicNode(ctx, id)
		if err != nil {
			return false, err
		}
		return node != nil, nil
	}
}

// serviceTokenAccess 组装 Bearer 数据面所需的认证器、限流器、可见性与指标；
// 未初始化服务令牌存储时返回空配置，浏览器与匿名路径保持不变。
func serviceTokenAccess(services *serverServices, metrics *observability.Registry, nodes nodeVisibility) api.ServiceTokenAccess {
	if services == nil || services.serviceTokenAuthenticator == nil {
		return api.ServiceTokenAccess{}
	}
	return api.ServiceTokenAccess{
		Authenticator:  services.serviceTokenAuthenticator,
		Limiter:        services.serviceTokenLimiter,
		Metrics:        metrics,
		Visibility:     nodeResourceVisibility(nodes),
		Audit:          serviceTokenAudit(services),
		FailureLimiter: services.serviceTokenFailureLimiter,
	}
}

// serviceTokenAudit 复用管理员审计存储记录服务主体事件；未初始化时返回 nil，
// 避免把 nil *adminauth.Service 装进接口导致运行期 panic。
func serviceTokenAudit(services *serverServices) middleware.ServiceTokenAudit {
	if services == nil || services.security == nil {
		return nil
	}
	return services.security
}

// serviceTokenAccessRouterOption 把数据面配置接入路由。
func serviceTokenAccessRouterOption(services *serverServices, metrics *observability.Registry, nodes nodeVisibility) api.RouterOption {
	return api.WithServiceTokenAccess(serviceTokenAccess(services, metrics, nodes))
}

func servicePrincipalRouterOption(services *serverServices) api.RouterOption {
	return api.WithServicePrincipals(services.servicePrincipals)
}
