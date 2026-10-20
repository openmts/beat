package api

import "net/http"

type runtimeAuditRoute struct{ action, kind, idField string }

// 运行时诊断审计动作固定；命中的写操作不再重复写入通用 admin.mutation 记录。
var runtimeDiagnosticsAtomicWrites = map[string]runtimeAuditRoute{
	"POST /api/v1/admin/runtime/captures":              {"runtime.capture.start", "runtime_capture", ""},
	"GET /api/v1/admin/runtime/captures/{id}/download": {"runtime.capture.download", "runtime_capture", "id"},
	"DELETE /api/v1/admin/runtime/captures/{id}":       {"runtime.capture.cancel", "runtime_capture", "id"},
}

func runtimeDiagnosticsAuditPolicy(request *http.Request) (string, string, string) {
	route, exists := runtimeDiagnosticsAtomicWrites[request.Pattern]
	if !exists {
		return "", "", ""
	}
	return route.action, route.kind, request.PathValue(route.idField)
}
