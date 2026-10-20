package serviceprincipal

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

var (
	// ErrName 表示服务令牌名称非法。
	ErrName = errors.New("service token name is invalid")
	// ErrPurpose 表示用途说明超长。
	ErrPurpose = errors.New("service token purpose is invalid")
	// ErrScope 表示权限集合为空或包含未知权限。
	ErrScope = errors.New("service token scope is invalid")
	// ErrResource 表示资源约束或可见范围非法。
	ErrResource = errors.New("service token resource constraint is invalid")
	// ErrSource 表示来源地址限制非法。
	ErrSource = errors.New("service token source restriction is invalid")
	// ErrExpiry 表示有效期超出允许边界。
	ErrExpiry = errors.New("service token expiry is invalid")
)

// IssueRequest 汇总创建或轮换服务令牌时的原始输入。
type IssueRequest struct {
	Name        string
	Purpose     string
	Scopes      []model.ServiceScope
	Resources   []model.ServiceResource
	Visibility  string
	SourceCIDRs []string
	ExpiresAt   time.Time
	Extended    bool
}

// Policy 是校验并规范化后的持久化策略字段。
type Policy struct {
	Name        string
	Purpose     string
	Scopes      []model.ServiceScope
	Resources   []model.ServiceResource
	Visibility  model.ServiceVisibility
	SourceCIDRs []string
	ExpiresAt   time.Time
}

// Validate 规范化输入并拒绝越权、越界与非法约束。
func (request IssueRequest) Validate(now time.Time) (Policy, error) {
	name, err := normalizeTokenName(request.Name)
	if err != nil {
		return Policy{}, err
	}
	if len(request.Purpose) > model.MaximumServiceTokenPurpose {
		return Policy{}, ErrPurpose
	}
	scopes, err := normalizeScopes(request.Scopes)
	if err != nil {
		return Policy{}, err
	}
	resources, err := normalizeResources(request.Resources, scopes)
	if err != nil {
		return Policy{}, err
	}
	visibility, err := normalizeVisibility(request.Visibility)
	if err != nil {
		return Policy{}, err
	}
	sources, err := normalizeSourceCIDRs(request.SourceCIDRs)
	if err != nil {
		return Policy{}, err
	}
	expires, err := normalizeExpiry(request.ExpiresAt, now, request.Extended)
	if err != nil {
		return Policy{}, err
	}
	return Policy{Name: name, Purpose: request.Purpose, Scopes: scopes, Resources: resources,
		Visibility: visibility, SourceCIDRs: sources, ExpiresAt: expires}, nil
}

func normalizeTokenName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > model.MaximumServiceTokenName || strings.ContainsRune(trimmed, 0) {
		return "", ErrName
	}
	return trimmed, nil
}

func normalizeScopes(scopes []model.ServiceScope) ([]model.ServiceScope, error) {
	if len(scopes) == 0 {
		return nil, ErrScope
	}
	unique := make([]model.ServiceScope, 0, len(scopes))
	for _, scope := range scopes {
		if !scope.Valid() {
			return nil, ErrScope
		}
		if !slices.Contains(unique, scope) {
			unique = append(unique, scope)
		}
	}
	slices.Sort(unique)
	return unique, nil
}

func normalizeResources(resources []model.ServiceResource, scopes []model.ServiceScope) ([]model.ServiceResource, error) {
	if len(resources) > model.MaximumServiceTokenResources {
		return nil, ErrResource
	}
	unique := make([]model.ServiceResource, 0, len(resources))
	for _, resource := range resources {
		if !resource.Kind.Valid() || !validResourceID(resource.ID) || !resourceKindAllowed(resource.Kind, scopes) {
			return nil, ErrResource
		}
		if !slices.Contains(unique, resource) {
			unique = append(unique, resource)
		}
	}
	slices.SortFunc(unique, func(left, right model.ServiceResource) int {
		if left.Kind != right.Kind {
			return strings.Compare(string(left.Kind), string(right.Kind))
		}
		return strings.Compare(left.ID, right.ID)
	})
	return unique, nil
}

func validResourceID(id string) bool {
	if id == "" || len(id) > model.MaximumServiceTokenIDLength || strings.ContainsRune(id, 0) {
		return false
	}
	return !strings.ContainsAny(id, "\r\n\t")
}

func resourceKindAllowed(kind model.ServiceResourceKind, scopes []model.ServiceScope) bool {
	allowed := resourceScopes(kind)
	for _, scope := range scopes {
		if slices.Contains(allowed, scope) {
			return true
		}
	}
	return false
}

func resourceScopes(kind model.ServiceResourceKind) []model.ServiceScope {
	switch kind {
	case model.ServiceResourceNode, model.ServiceResourceGroup:
		return []model.ServiceScope{model.ServiceScopeNodesRead, model.ServiceScopeNodesWritePresentation,
			model.ServiceScopeMetricsRead, model.ServiceScopeNetworkTasksRead, model.ServiceScopeNetworkTasksRunTest,
			model.ServiceScopeAlertsRead, model.ServiceScopeTrafficReportsRead, model.ServiceScopeTrafficReportsRunTest}
	case model.ServiceResourceChannel:
		return []model.ServiceScope{model.ServiceScopeAlertsRead, model.ServiceScopeNotificationTestSend}
	default:
		return nil
	}
}

func normalizeVisibility(value string) (model.ServiceVisibility, error) {
	if value == "" {
		return model.ServiceVisibilityPrivate, nil
	}
	visibility := model.ServiceVisibility(value)
	if !visibility.Valid() {
		return "", ErrResource
	}
	return visibility, nil
}

func normalizeSourceCIDRs(values []string) ([]string, error) {
	if len(values) > model.MaximumServiceTokenCIDRs {
		return nil, ErrSource
	}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, ErrSource
		}
		canonical := prefix.Masked().String()
		if !slices.Contains(unique, canonical) {
			unique = append(unique, canonical)
		}
	}
	slices.Sort(unique)
	return unique, nil
}

func normalizeExpiry(expiresAt time.Time, now time.Time, extended bool) (time.Time, error) {
	if expiresAt.IsZero() {
		return time.Time{}, ErrExpiry
	}
	expires := expiresAt.UTC()
	limit := model.DefaultServiceTokenLifetime
	if extended {
		limit = model.ExtendedServiceTokenLifetime
	}
	if !expires.After(now.UTC()) || expires.After(now.UTC().Add(limit)) {
		return time.Time{}, ErrExpiry
	}
	return expires, nil
}
