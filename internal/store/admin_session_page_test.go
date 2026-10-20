package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func seedSession(t *testing.T, database *AdminStore, id string, createdAt time.Time, expiry time.Time, revokedAt *time.Time) {
	t.Helper()
	session := model.AdminSession{
		ID: id, UserID: "security-owner", TokenHash: []byte(id), TokenPrefix: id,
		CreatedAt: createdAt, LastActivityAt: createdAt, IdleExpiresAt: expiry, AbsoluteExpiresAt: expiry,
		RevokedAt: revokedAt,
	}
	if err := database.CreateSession(t.Context(), &session); err != nil {
		t.Fatalf("seed session %s: %v", id, err)
	}
}

func TestListSessionsBoundedAndRecentlyRevoked(t *testing.T) {
	database, _ := sessionSecurityFixture(t)
	now := model.NowUTC()
	expiry := now.Add(time.Hour)
	// fixture 已有一个活跃会话；再补 59 个活跃会话 + 2 个刚刚吊销 + 1 个久远吊销 + 1 个已过期。
	for index := range 59 {
		seedSession(t, database, "active-"+string(rune('a'+index%26))+string(rune('0'+index/26)),
			now.Add(-time.Duration(index+1)*time.Minute), expiry, nil)
	}
	recent := now.Add(-time.Hour)
	seedSession(t, database, "revoked-recent", now.Add(-2*time.Hour), expiry, &recent)
	seedSession(t, database, "revoked-recent-two", now.Add(-3*time.Hour), expiry, &recent)
	stale := now.Add(-72 * time.Hour)
	seedSession(t, database, "revoked-stale", now.Add(-4*time.Hour), expiry, &stale)
	seedSession(t, database, "expired", now.Add(-5*time.Hour), now.Add(-time.Minute), nil)

	page, err := database.ListSessions(t.Context(), "security-owner", now, model.AdminSessionMaximumPage)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	// 60 个活跃会话（fixture 1 + 59）加 2 个刚刚吊销的会话；久远吊销与已过期的必须被排除。
	if len(page) != 62 {
		t.Fatalf("expected 62 visible sessions, got %d", len(page))
	}
	byID := map[string]model.AdminSession{}
	for _, session := range page {
		byID[session.ID] = session
	}
	for _, excluded := range []string{"revoked-stale", "expired"} {
		if _, found := byID[excluded]; found {
			t.Fatalf("expected %s to be excluded from the bounded page", excluded)
		}
	}
	if _, found := byID["revoked-recent"]; !found {
		t.Fatal("expected a recently revoked session to remain visible")
	}
	if page[0].RevokedAt != nil || page[61].RevokedAt == nil {
		t.Fatalf("expected active sessions before revoked ones, got %+v", page)
	}

	tiny, err := database.ListSessions(t.Context(), "security-owner", now, 3)
	if err != nil || len(tiny) != 3 {
		t.Fatalf("expected an explicit page size to bound the page, got %d / %v", len(tiny), err)
	}
	if tiny[0].RevokedAt != nil {
		t.Fatalf("expected active sessions first, got %+v", tiny[0])
	}
	if other, err := database.ListSessions(t.Context(), "missing-user", now, 10); err != nil || len(other) != 0 {
		t.Fatalf("expected no sessions for an unknown user, got %+v / %v", other, err)
	}
}

func TestLocalAdmissionRevokesOldestSessionsAboveCap(t *testing.T) {
	database, admission := localAdmissionFixture(t, false)
	now := admission.Session.CreatedAt
	expiry := now.Add(time.Hour)
	// 40 个额外活跃会话 + fixture 自身的 1 个 = 41 个；登录后共 42 个，应裁到上限。
	for index := range 40 {
		seedSession(t, database, "extra-"+string(rune('a'+index%26))+string(rune('0'+index/26)),
			now.Add(-time.Duration(index+1)*time.Minute), expiry, nil)
	}
	if err := database.AdmitLocalSession(t.Context(), admission); err != nil {
		t.Fatalf("admit local session: %v", err)
	}
	var active int
	if err := database.db.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE user_id = ? AND revoked_at IS NULL
		AND idle_expires_at > ? AND absolute_expires_at > ?`, admission.User.ID, now, now).Scan(&active); err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if active != model.MaxActiveAdminSessions {
		t.Fatalf("expected the cap of %d active sessions, got %d", model.MaxActiveAdminSessions, active)
	}
	page, err := database.ListAuditEvents(t.Context(), model.AuditFilter{Action: "session.cap_revoke", Limit: 10})
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("expected one cap revocation audit event, got %+v / %v", page, err)
	}
	if page.Events[0].DetailJSON != `{"revoked":10,"cap":32}` {
		t.Fatalf("unexpected cap audit detail %q", page.Events[0].DetailJSON)
	}
	// 最旧的额外会话必须已被吊销，最新登录的会话必须保留。
	var revoked int
	if err := database.db.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE revoked_at IS NOT NULL`).Scan(&revoked); err != nil {
		t.Fatalf("count revoked sessions: %v", err)
	}
	if revoked != 10 {
		t.Fatalf("expected exactly the excess sessions to be revoked, got %d", revoked)
	}
}

func TestLocalAdmissionBelowCapWritesNoCapAudit(t *testing.T) {
	database, admission := localAdmissionFixture(t, false)
	if err := database.AdmitLocalSession(t.Context(), admission); err != nil {
		t.Fatalf("admit local session: %v", err)
	}
	page, err := database.ListAuditEvents(t.Context(), model.AuditFilter{Action: "session.cap_revoke"})
	if err != nil || len(page.Events) != 0 {
		t.Fatalf("expected no cap audit event, got %+v / %v", page, err)
	}
}
