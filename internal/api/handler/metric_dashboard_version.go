package handler

import (
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// HandleListVersions 返回看板的历史版本元数据（不含定义体），按版本号从新到旧。
func (handler *MetricDashboardHandler) HandleListVersions(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	target, err := handler.versionDashboard(request, actor)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	values, err := handler.dashboards.ListVersions(request.Context(), target.ID)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, values)
}

// HandleVersionDefinition 返回某个历史版本的完整定义，供查看与差异对比。
func (handler *MetricDashboardHandler) HandleVersionDefinition(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	target, err := handler.versionDashboard(request, actor)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	revision, err := dashboardVersionPath(request)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	definition, err := handler.dashboards.VersionDefinition(request.Context(), target.ID, revision)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, definition)
}

// HandleRestoreVersion 把看板恢复到指定历史版本。恢复本身会写入一个新版本，
// 因此恢复动作也能被再次恢复；body.revision 仍是客户端看到的当前版本号（乐观并发）。
func (handler *MetricDashboardHandler) HandleRestoreVersion(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	value, err := handler.writableDashboard(request, actor)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	revision, err := dashboardVersionPath(request)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	var body struct {
		Revision int    `json:"revision"`
		Summary  string `json:"summary"`
	}
	if err := decodeDashboardRequest(w, request, &body); err != nil {
		respondDashboardError(w, err)
		return
	}
	if err := model.ValidateDashboardVersionSummary(body.Summary); err != nil {
		respondDashboardError(w, err)
		return
	}
	definition, err := handler.dashboards.VersionDefinition(request.Context(), value.ID, revision)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	value.Definition = *definition
	if err := handler.validatePolicy(request, *value); err != nil {
		respondDashboardError(w, err)
		return
	}
	restored, err := handler.dashboards.RestoreVersion(request.Context(), store.DashboardVersionRestore{
		ID: value.ID, Revision: revision, ExpectedRevision: body.Revision, Summary: body.Summary, Actor: actor})
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, restored)
}

// dashboardVersionPath 解析路径里的历史版本号；非正整数按无效看板处理。
func dashboardVersionPath(request *http.Request) (int, error) {
	revision, err := strconv.Atoi(request.PathValue("revision"))
	if err != nil || revision <= 0 {
		return 0, model.ErrInvalidDashboard
	}
	return revision, nil
}

// versionDashboard 解析版本历史的只读目标：沿用写接口的权限模型（个人看板限本人，
// 共享看板要求 owner），但只读动作不要求最近认证，避免「看历史」也要重新验身。
func (handler *MetricDashboardHandler) versionDashboard(request *http.Request, actor store.DashboardActor) (*model.MetricDashboard, error) {
	value, err := handler.dashboards.Get(request.Context(), request.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if value == nil {
		value = dashboardTarget(request.PathValue("id"))
	}
	if value == nil {
		return nil, store.ErrDashboardNotFound
	}
	if value.Scope == "personal" && value.OwnerUserID != actor.UserID {
		return nil, store.ErrDashboardNotFound
	}
	if value.Scope != "personal" {
		principal, ok := middleware.AdminPrincipal(request.Context())
		if !ok || principal.User.Role != model.AdminRoleOwner {
			return nil, store.ErrDashboardForbidden
		}
	}
	return value, nil
}
