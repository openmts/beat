package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type metadataFixture struct {
	db     *sql.DB
	ctx    context.Context
	nodes  *NodeStore
	groups *GroupStore
	site   *SiteSettingsStore
	node   *model.Node
	group  *model.Group
}

func newMetadataFixture(t *testing.T) metadataFixture {
	t.Helper()
	sqlite, err := NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	now := model.NowUTC()
	admins := NewAdminStore(sqlite.DB)
	user := model.AdminUser{ID: "metadata-admin", Username: "metadata-admin", DisplayName: "Admin", Role: model.AdminRoleAdmin, Enabled: true, PasswordHash: "fixture", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Hour)
	session := model.AdminSession{ID: "metadata-session", UserID: user.ID, TokenHash: bytes.Repeat([]byte{1}, 32), TokenPrefix: "prefix", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}
	if err := admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	f := metadataFixture{db: sqlite.DB, nodes: NewNodeStore(sqlite.DB), groups: NewGroupStore(sqlite.DB), site: NewSiteSettingsStore(sqlite.DB)}
	f.ctx = WithMetadataActor(t.Context(), MetadataActor{DashboardActor: DashboardActor{UserID: user.ID, SessionID: session.ID, RequestID: "metadata-request", IPAddress: "127.0.0.1", UserAgent: "agent"}})
	f.group, err = f.groups.CreateGroup(t.Context(), "Original")
	if err != nil {
		t.Fatal(err)
	}
	f.node, err = f.nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "managed", Host: "127.0.0.1", Port: 22, GroupID: f.group.ID}, AgentCredential{Hash: bytes.Repeat([]byte{2}, 32), Prefix: "old", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f metadataFixture) mutate(t *testing.T, kind string) error {
	t.Helper()
	switch kind {
	case "group.create":
		_, err := f.groups.CreateGroup(f.ctx, "New group")
		return err
	case "group.update":
		return f.groups.UpdateGroup(f.ctx, f.group.ID, "New name")
	case "group.delete":
		return f.groups.DeleteGroup(f.ctx, f.group.ID)
	case "group.default":
		return f.groups.SetDefaultGroup(f.ctx, f.group.ID)
	case "group.sort":
		return f.groups.UpdateSortOrder(f.ctx, []string{f.group.ID})
	case "node.create":
		_, err := f.nodes.CreateManagedNode(f.ctx, ManagedNodeInput{Name: "new", Host: "127.0.0.2", Port: 22}, AgentCredential{Hash: bytes.Repeat([]byte{3}, 32), Prefix: "new", CreatedAt: model.NowUTC()})
		return err
	case "node.update":
		remark := "private-do-not-audit"
		_, err := f.nodes.UpdateNode(f.ctx, f.node.ID, NodeUpdate{Alias: "changed", GroupID: f.group.ID, PrivateRemark: &remark})
		return err
	case "node.sort":
		return f.nodes.UpdateNodeSort(f.ctx, []string{f.node.ID})
	case "node.token.rotate":
		_, err := f.nodes.RotateAgentToken(f.ctx, f.node.ID, AgentCredential{Hash: bytes.Repeat([]byte{4}, 32), Prefix: "new", CreatedAt: model.NowUTC()})
		return err
	case "node.token.revoke":
		_, err := f.nodes.RevokeAgentToken(f.ctx, f.node.ID, model.NowUTC())
		return err
	case "site_settings.update":
		settings := model.DefaultSiteSettings()
		settings.SiteTitle = "Changed"
		_, err := f.site.Update(f.ctx, settings)
		return err
	}
	t.Fatal("unknown mutation", kind)
	return nil
}

func (f metadataFixture) snapshot(t *testing.T) string {
	t.Helper()
	groups, err := f.groups.ListGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := f.nodes.ListNodes(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := f.site.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal([]any{groups, nodes, settings})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var metadataActions = []string{"group.create", "group.update", "group.delete", "group.default", "group.sort", "node.create", "node.update", "node.sort", "node.token.rotate", "node.token.revoke", "site_settings.update"}

func TestMetadataMutationsCommitExactAudit(t *testing.T) {
	for _, action := range metadataActions {
		t.Run(action, func(t *testing.T) {
			f := newMetadataFixture(t)
			if err := f.mutate(t, action); err != nil {
				t.Fatal(err)
			}
			var actual, actor, request, resource, detail string
			if err := f.db.QueryRow("SELECT action,actor_id,request_id,resource_id,detail_json FROM admin_audit_events").Scan(&actual, &actor, &request, &resource, &detail); err != nil {
				t.Fatal(err)
			}
			if actual != action || actor != "metadata-admin" || request != "metadata-request" || resource == "" {
				t.Fatal(actual, actor, request, resource)
			}
			if bytes.Contains([]byte(detail), []byte("private-do-not-audit")) {
				t.Fatal("private value entered audit")
			}
		})
	}
}

func TestMetadataAuditFailureRollsBackEveryDomain(t *testing.T) {
	for _, action := range metadataActions {
		t.Run(action, func(t *testing.T) {
			f := newMetadataFixture(t)
			before := f.snapshot(t)
			if _, err := f.db.Exec(`CREATE TRIGGER fail_metadata_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'full');END`); err != nil {
				t.Fatal(err)
			}
			if err := f.mutate(t, action); err == nil {
				t.Fatal("unaudited mutation committed")
			}
			if after := f.snapshot(t); after != before {
				t.Fatal("partial mutation survived failed audit")
			}
		})
	}
}
