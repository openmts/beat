package serviceprincipal

import (
	"net/netip"
	"slices"
	"time"

	"github.com/beat/backend/internal/model"
)

// Usable 判断令牌在指定时刻是否仍然有效：未撤销、未过期且轮换重叠窗口未结束。
func Usable(principal model.ServicePrincipal, at time.Time) bool {
	if principal.RevokedAt != nil {
		return false
	}
	if !principal.ExpiresAt.After(at) {
		return false
	}
	if principal.SupersededAt == nil {
		return true
	}
	deadline := principal.SupersededAt.Add(time.Duration(principal.RotationOverlapSeconds) * time.Second)
	return deadline.After(at)
}

// AllowsSource 判断来源地址是否满足令牌的 CIDR 约束；空约束表示不限制来源。
func AllowsSource(principal model.ServicePrincipal, address netip.Addr) bool {
	if len(principal.SourceCIDRs) == 0 {
		return true
	}
	if !address.IsValid() {
		return false
	}
	canonical := address.Unmap()
	for _, cidr := range principal.SourceCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		if prefix.Contains(canonical) {
			return true
		}
	}
	return false
}

// Request 描述一次受控操作所需的权限与资源。
type Request struct {
	Scope model.ServiceScope
	Kind  model.ServiceResourceKind
	ID    string
}

// Permits 在权限匹配后再求资源约束交集；某一类型没有任何约束时表示不限制。
func Permits(principal model.ServicePrincipal, request Request) bool {
	if !slices.Contains(principal.Scopes, request.Scope) {
		return false
	}
	if request.Kind == "" {
		return true
	}
	var scoped []model.ServiceResource
	for _, resource := range principal.Resources {
		if resource.Kind == request.Kind {
			scoped = append(scoped, resource)
		}
	}
	if len(scoped) == 0 {
		return true
	}
	if request.ID == "" {
		return false
	}
	return slices.Contains(scoped, model.ServiceResource{Kind: request.Kind, ID: request.ID})
}
