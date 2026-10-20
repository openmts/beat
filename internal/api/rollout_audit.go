package api

import "net/http"

type rolloutAuditEntry struct{ action, kind, idField string }

var rolloutAtomicWrites = map[string]rolloutAuditEntry{
	"POST /api/v1/admin/agent-releases/import":               {"release.import", "release", ""},
	rolloutUploadPattern:                                     {"release.artifact.import", "release", "release_id"},
	"POST /api/v1/admin/agent-releases/{release_id}/revoke":  {"release.revoke", "release", "release_id"},
	"POST /api/v1/admin/agent-rollouts":                      {"rollout.create", "rollout", ""},
	"POST /api/v1/admin/agent-rollouts/{rollout_id}/promote": {"rollout.promote", "rollout", "rollout_id"},
	"POST /api/v1/admin/agent-rollouts/{rollout_id}/pause":   {"rollout.paused", "rollout", "rollout_id"},
	"POST /api/v1/admin/agent-rollouts/{rollout_id}/resume":  {"rollout.resume", "rollout", "rollout_id"},
	"POST /api/v1/admin/agent-rollouts/{rollout_id}/abort":   {"rollout.aborted", "rollout", "rollout_id"},
}

func rolloutAuditPolicy(request *http.Request) (string, string, string) {
	policy, exists := rolloutAtomicWrites[request.Pattern]
	if !exists {
		return "", "", ""
	}
	return policy.action, policy.kind, request.PathValue(policy.idField)
}

func atomicRouteAuditPolicy(request *http.Request) (string, string, string) {
	if action, kind, id := rolloutAuditPolicy(request); action != "" {
		return action, kind, id
	}
	if action, kind, id := runtimeDiagnosticsAuditPolicy(request); action != "" {
		return action, kind, id
	}
	return metadataAuditPolicy(request)
}
