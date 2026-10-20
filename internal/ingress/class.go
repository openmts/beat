// Package ingress 提供与具体路由无关的入口准入控制：固定类别、容量配置、
// 全局与身份令牌桶、在途租约以及只读统计。
//
// 设计约束见 docs/beat/ingress-resource-governance-design.md：公共监控无需登录，
// 管理面仍需会话与授权保护；限流状态只存在于内存，不占用 SQLite 迁移，
// 备份格式保持 v4 不变。
package ingress

import (
	"net/http"
	"strings"
)

// Class 是入口准入的固定类别。调用方不能自行拼装限流器，只能选用这里的枚举。
type Class uint8

const (
	PublicStatic Class = iota
	PublicSnapshot
	PublicHistory
	AuthKDF
	AgentRead
	AgentWrite
	AdminRead
	AdminWrite
	AdminExpensive

	classCount
)

const apiPrefix = "/api/v1/"

var classNames = [classCount]string{
	"public_static",
	"public_snapshot",
	"public_history",
	"auth_kdf",
	"agent_read",
	"agent_write",
	"admin_read",
	"admin_write",
	"admin_expensive",
}

// String 返回稳定的类别名，可直接作为 Prometheus 标签与 JSON 字段。
func (class Class) String() string {
	if !class.Valid() {
		return "unknown"
	}
	return classNames[class]
}

// Valid 判断类别是否属于固定枚举。
func (class Class) Valid() bool {
	return class < classCount
}

// Classes 按固定顺序返回全部类别，保证指标与只读视图输出稳定。
func Classes() []Class {
	classes := make([]Class, 0, classCount)
	for class := Class(0); class < classCount; class++ {
		classes = append(classes, class)
	}
	return classes
}

// 昂贵的入口路径：上传、导出、抓取与长耗时作业。它们共用 admin_expensive 预算，
// 读写都算昂贵，因为一次导出或抓取会占用共享的磁盘与 MTS 预算。
var expensiveAdminFragments = []string{
	"/backups/uploads",
	"/runtime/captures",
	"/runtime/summary",
	"/diagnostics/jobs",
	"/themes/upload",
	"/export",
	"/download",
	"/geoip",
}

// 只有写入才昂贵的路径：列出任务或策略是普通管理读。
var expensiveAdminWriteFragments = []string{
	"/theme-markets",
	"/metric-erasure/jobs",
	"/operations/tasks",
	"/operations/transfers",
}

// Classify 根据方法与路径判定准入类别。ok=false 表示该请求不受准入控制，
// 例如前端静态资源与由 runtime-resilience 拥有的 WebSocket 通道（Agent 通道与
// 公共指标流）：长连接有自己的连接数、握手速率与在途预算，不占用 HTTP 令牌桶，
// 也不把长连接的整个生命周期计成一次在途请求。
//
// 未识别的 API 路径一律落到管理预算，而不是更宽松的公共预算：新增路由默认受限，
// 必须显式加入公共白名单才能获得公共配额。
func Classify(method, path string) (Class, bool) {
	if !strings.HasPrefix(path, apiPrefix) {
		return 0, false
	}
	target := strings.TrimSuffix(path, "/")
	switch {
	case authKDFPath(target):
		return AuthKDF, true
	case target == "/api/v1/nodes/report", target == "/api/v1/network/results":
		return AgentWrite, true
	case target == "/api/v1/network/assignments":
		return AgentRead, true
	case websocketPath(target):
		return 0, false
	case publicWritePath(target):
		return PublicStatic, true
	case strings.HasPrefix(target, "/api/v1/admin/"):
		return adminClass(method, target), true
	case strings.HasPrefix(target, "/api/v1/network/tasks"):
		return adminClass(method, target), true
	case publicMetricPath(target):
		return publicMetricClass(method, target), true
	case strings.HasSuffix(target, "/history"):
		return PublicHistory, true
	case publicStaticPath(target):
		return publicStaticClass(method, target), true
	case publicSnapshotPath(target):
		return publicSnapshotClass(method, target), true
	default:
		return adminClass(method, target), true
	}
}

// websocketPath 列出由通道自身准入控制的 WebSocket 升级入口。它们既不是管理面额度，
// 也不是按请求计费的公共读额度。
func websocketPath(target string) bool {
	switch target {
	case "/api/v1/agents/ws", "/api/v1/ws/metrics":
		return true
	default:
		return false
	}
}

// publicMetricPath 是公共指标工作区的匿名只读接口；管理面在 /api/v1/admin/metrics/ 下，
// 与前缀判断分开，避免公共页面占用管理额度。
func publicMetricPath(target string) bool {
	switch target {
	case "/api/v1/metrics/catalog", "/api/v1/metrics/query", "/api/v1/metrics/dashboards/default",
		"/api/v1/metrics/annotations":
		return true
	default:
		return strings.HasPrefix(target, "/api/v1/metrics/snapshots/")
	}
}

// publicMetricClass 把目录、看板定义、注释与快照放进公共静态预算，把时间序列查询放进
// 公共历史预算：两者都是匿名读，按设计文档不与管理面共享令牌桶。
func publicMetricClass(method, target string) Class {
	if target == "/api/v1/metrics/query" && method == http.MethodPost {
		return PublicHistory
	}
	return publicStaticClass(method, target)
}

// publicStaticPath 是站点/分组元数据、活动主题与公开认证元数据这类小型只读响应。
func publicStaticPath(target string) bool {
	switch target {
	case "/api/v1/settings/site", "/api/v1/groups", "/api/v1/themes/active",
		"/api/v1/auth/state", "/api/v1/auth/oidc/providers", "/api/v1/auth/oidc/callback":
		return true
	default:
		return false
	}
}

// publicWritePath 是保留在公共静态预算里的匿名写端点。
func publicWritePath(target string) bool {
	return target == "/api/v1/public/interactions"
}

// publicSnapshotPath 是公共 Fleet/节点/网络质量的当前视图。
func publicSnapshotPath(target string) bool {
	if target == "/api/v1/network/quality" || strings.HasPrefix(target, "/api/v1/network/quality/") {
		return true
	}
	if target == "/api/v1/nodes" || strings.HasPrefix(target, "/api/v1/nodes/") {
		return !privateNodePath(target)
	}
	return false
}

// privateNodePath 在上报、纳管、安装配置与令牌接口上保持管理预算。
func privateNodePath(target string) bool {
	if target == "/api/v1/nodes/manage" || target == "/api/v1/nodes/report" || target == "/api/v1/nodes/sort" {
		return true
	}
	return strings.HasSuffix(target, "/install") || strings.Contains(target, "/token/")
}

func publicStaticClass(method, target string) Class {
	if readMethod(method) {
		return PublicStatic
	}
	return adminClass(method, target)
}

func publicSnapshotClass(method, target string) Class {
	if readMethod(method) {
		return PublicSnapshot
	}
	return adminClass(method, target)
}

// adminClass 区分管理读、管理写与昂贵操作，避免把大上传挤进普通写预算。
func adminClass(method, target string) Class {
	if containsFragment(target, expensiveAdminFragments) {
		return AdminExpensive
	}
	if readMethod(method) {
		return AdminRead
	}
	if containsFragment(target, expensiveAdminWriteFragments) {
		return AdminExpensive
	}
	return AdminWrite
}

func containsFragment(target string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(target, fragment) {
			return true
		}
	}
	return false
}

// authKDFPath 列出需要先于口令散列与账号查询取号的入口。
func authKDFPath(target string) bool {
	switch target {
	case "/api/v1/auth/login", "/api/v1/auth/bootstrap",
		"/api/v1/auth/reauthenticate", "/api/v1/admin/users/me/password":
		return true
	default:
		return false
	}
}

func readMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}
