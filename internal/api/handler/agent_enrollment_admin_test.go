package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const discoveryTestOrigin = "https://beat.example.com"

type discoveryHarness struct {
	admin     *AgentEnrollmentAdminHandler
	discovery *AgentDiscoveryHandler
	store     *store.AgentEnrollmentStore
	db        *sql.DB
	now       time.Time
	groupID   string
}

// newDiscoveryHarness 打开真实 SQLite 夹具并构造管理端与协议处理器。
func newDiscoveryHarness(t *testing.T) discoveryHarness {
	t.Helper()
	database, err := store.NewSQLiteStore(handlerFixturePath(t))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	groups := store.NewGroupStore(database.DB)
	group, err := groups.CreateGroup(t.Context(), "enrollment-target")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	enrollment := store.NewAgentEnrollmentStore(database.DB)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	admin := NewAgentEnrollmentAdminHandler(enrollment)
	admin.now = func() time.Time { return now }
	discovery := NewAgentDiscoveryHandler(enrollment, func(*http.Request) string { return discoveryTestOrigin }, false)
	discovery.now = func() time.Time { return now }
	return discoveryHarness{admin: admin, discovery: discovery, store: enrollment, db: database.DB,
		now: now, groupID: group.ID}
}

// enrollmentAdminRequest 构造带 owner 主体的请求，模拟已通过路由层校验的管理会话。
func enrollmentAdminRequest(method, target string, body any) *http.Request {
	payload, _ := json.Marshal(body)
	request := httptest.NewRequest(method, target, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	principal := model.AdminPrincipal{User: model.AdminUser{ID: "owner-1", Role: model.AdminRoleOwner}}
	return request.WithContext(middleware.WithAdminPrincipal(context.Background(), principal))
}

func grantPolicyPayload(now time.Time, groupID string) enrollmentGrantUpsertPayload {
	return enrollmentGrantUpsertPayload{Name: "edge-fleet", Mode: "manual", Enabled: true,
		ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339), DefaultGroupID: groupID, SSHPort: 22,
		MaximumEnrollments: 2}
}

func decodeEnrollmentBody[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response %s: %v", recorder.Body.String(), err)
	}
	return envelope.Data
}

func TestAgentEnrollmentGrantAdminLifecycle(t *testing.T) {
	harness := newDiscoveryHarness(t)
	recorder := httptest.NewRecorder()
	harness.admin.HandleCreateGrant(recorder, enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants", grantPolicyPayload(harness.now, harness.groupID)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create grant status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	created := decodeEnrollmentBody[enrollmentGrantSecretResponse](t, recorder)
	if created.Secret == "" || created.Grant.ID == "" || created.Grant.Mode != "manual" {
		t.Fatalf("created grant = %#v", created)
	}
	if _, err := agentenrollment.DescribeSecret(agentenrollment.GrantSecretPrefix, created.Secret); err != nil {
		t.Fatalf("generated secret is not well formed: %v", err)
	}
	if created.Grant.SecretPrefix == created.Secret || len(created.Grant.SecretPrefix) >= len(created.Secret) {
		t.Fatalf("display prefix leaked the whole secret: %q", created.Grant.SecretPrefix)
	}
	resolved, err := harness.store.GrantBySecret(t.Context(), created.Secret, harness.now)
	if err != nil || resolved.ID != created.Grant.ID {
		t.Fatalf("secret did not resolve to its grant: %#v err=%v", resolved, err)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleListGrants(recorder, enrollmentAdminRequest(http.MethodGet, "/api/v1/admin/agent-enrollment/grants", nil))
	listed := decodeEnrollmentBody[enrollmentGrantListResponse](t, recorder)
	if recorder.Code != http.StatusOK || len(listed.Grants) != 1 || listed.Grants[0].ID != created.Grant.ID {
		t.Fatalf("list grants = %d %#v", recorder.Code, listed)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleGetGrant(recorder, withPath(enrollmentAdminRequest(http.MethodGet,
		"/api/v1/admin/agent-enrollment/grants/"+created.Grant.ID, nil), "id", created.Grant.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get grant status = %d", recorder.Code)
	}

	update := grantPolicyPayload(harness.now, harness.groupID)
	update.MaximumEnrollments = 3
	recorder = httptest.NewRecorder()
	harness.admin.HandleUpdateGrant(recorder, withPath(enrollmentAdminRequest(http.MethodPut,
		"/api/v1/admin/agent-enrollment/grants/"+created.Grant.ID, update), "id", created.Grant.ID))
	updated := decodeEnrollmentBody[struct {
		Grant enrollmentGrantPayload `json:"grant"`
	}](t, recorder)
	if recorder.Code != http.StatusOK || updated.Grant.MaximumEnrollments != 3 {
		t.Fatalf("update grant = %d %#v", recorder.Code, updated)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleRotateGrantSecret(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants/"+created.Grant.ID+"/rotate", nil), "id", created.Grant.ID))
	rotated := decodeEnrollmentBody[enrollmentGrantSecretResponse](t, recorder)
	if recorder.Code != http.StatusOK || rotated.Secret == created.Secret {
		t.Fatalf("rotate grant = %d %#v", recorder.Code, rotated)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleRevokeGrant(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants/"+created.Grant.ID+"/revoke", nil), "id", created.Grant.ID))
	revoked := decodeEnrollmentBody[struct {
		Grant enrollmentGrantPayload `json:"grant"`
	}](t, recorder)
	if recorder.Code != http.StatusOK || revoked.Grant.RevokedAt == "" {
		t.Fatalf("revoke grant = %d %#v", recorder.Code, revoked)
	}
}

func TestAgentEnrollmentGrantAdminRejectsBadPayloads(t *testing.T) {
	harness := newDiscoveryHarness(t)
	payload := grantPolicyPayload(harness.now, harness.groupID)
	payload.ExpiresAt = "not-a-time"
	recorder := httptest.NewRecorder()
	harness.admin.HandleCreateGrant(recorder, enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants", payload))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid expiry status = %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleCreateGrant(recorder, enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants", grantPolicyPayload(harness.now, harness.groupID)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("first grant status = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	harness.admin.HandleCreateGrant(recorder, enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants", grantPolicyPayload(harness.now, harness.groupID)))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate name status = %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleGetGrant(recorder, withPath(enrollmentAdminRequest(http.MethodGet, "/grants/missing", nil), "id", "missing"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing grant status = %d", recorder.Code)
	}
}

// withPath 注入路径参数，等价于 http.ServeMux 的 {id} 匹配结果。
func withPath(request *http.Request, name, value string) *http.Request {
	request.SetPathValue(name, value)
	return request
}

// TestAgentEnrollmentAdminLifecycleWritesAuditRows 断言管理端写路径把已认证主体与请求来源
// 写进审计表，且审计行不含授权秘密明文。
func TestAgentEnrollmentAdminLifecycleWritesAuditRows(t *testing.T) {
	harness := newDiscoveryHarness(t)
	if _, err := harness.db.Exec("DELETE FROM admin_audit_events"); err != nil {
		t.Fatalf("clear audits: %v", err)
	}
	recorder := httptest.NewRecorder()
	harness.admin.HandleCreateGrant(recorder, enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants", grantPolicyPayload(harness.now, harness.groupID)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create grant status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	created := decodeEnrollmentBody[enrollmentGrantSecretResponse](t, recorder)
	recorder = httptest.NewRecorder()
	harness.admin.HandleRotateGrantSecret(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants/"+created.Grant.ID+"/rotate", nil), "id", created.Grant.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("rotate grant status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	rotated := decodeEnrollmentBody[enrollmentGrantSecretResponse](t, recorder)
	recorder = httptest.NewRecorder()
	harness.admin.HandleRevokeGrant(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants/"+created.Grant.ID+"/revoke", nil), "id", created.Grant.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("revoke grant status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	rows, err := harness.db.Query(`SELECT action, COALESCE(actor_id, ''), actor_username, detail_json
		FROM admin_audit_events WHERE action LIKE 'agent.enrollment.%' ORDER BY rowid`)
	if err != nil {
		t.Fatalf("query audits: %v", err)
	}
	defer func() { _ = rows.Close() }()
	actions := make([]string, 0, 3)
	details := make([]string, 0, 3)
	for rows.Next() {
		var action, actorID, username, detail string
		if err := rows.Scan(&action, &actorID, &username, &detail); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		if actorID != "owner-1" || username != "owner-1" {
			t.Fatalf("audit actor = %q/%q", actorID, username)
		}
		actions = append(actions, action)
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audits: %v", err)
	}
	want := []string{"agent.enrollment.grant.create", "agent.enrollment.grant.rotate", "agent.enrollment.grant.revoke"}
	if len(actions) != len(want) {
		t.Fatalf("audit actions = %#v", actions)
	}
	for index, action := range want {
		if actions[index] != action {
			t.Fatalf("audit %d = %q, want %q", index, actions[index], action)
		}
		if strings.Contains(details[index], created.Secret) || strings.Contains(details[index], rotated.Secret) {
			t.Fatalf("audit detail leaked a secret: %s", details[index])
		}
	}
}
