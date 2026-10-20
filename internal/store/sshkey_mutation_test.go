package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

type sshMutationFixture struct {
	dashboardFixture
	keys *SSHKeyStore
	ctx  context.Context
}

func newSSHMutationFixture(t *testing.T) sshMutationFixture {
	t.Helper()
	database := setupTestDB(t)
	keys := newTestSSHKeyStore(t, database.DB)
	f := dashboardFixture{database: database, admins: NewAdminStore(database.DB)}
	f.actor = f.addActor(t, "owner", model.AdminRoleOwner)
	return sshMutationFixture{dashboardFixture: f, keys: keys, ctx: WithMetadataActor(t.Context(), MetadataActor{DashboardActor: f.actor})}
}

func sshMutationInput() SSHKeyInput {
	return SSHKeyInput{Name: "Production SSH", KeyType: "ed25519", PublicKey: "fixture-public-secret", PrivateKey: "fixture-private-secret", Fingerprint: "fingerprint"}
}

func (f sshMutationFixture) exec(t *testing.T, statement string) {
	t.Helper()
	if _, err := f.database.DB.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

func TestSSHMutationAuditAndRedaction(t *testing.T) {
	for _, generated := range []bool{false, true} {
		t.Run(fmt.Sprint(generated), func(t *testing.T) {
			f := newSSHMutationFixture(t)
			input := sshMutationInput()
			input.Generated = generated
			key, err := f.keys.CreateSSHKey(f.ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			private, err := f.keys.ReadPrivateKey(f.ctx, key.ID)
			if err != nil || private != input.PrivateKey {
				t.Fatal("authorized reveal failed", err)
			}
			if err := f.keys.DeleteSSHKey(f.ctx, key.ID); err != nil {
				t.Fatal(err)
			}
			var count int
			var detail, actions string
			err = f.database.DB.QueryRowContext(t.Context(), "SELECT count(*),group_concat(detail_json),group_concat(action) FROM admin_audit_events WHERE resource_type='ssh_key' AND request_id='request-id'").Scan(&count, &detail, &actions)
			if err != nil || count != 3 {
				t.Fatal("expected three atomic audit events", count, err)
			}
			want := "ssh_key.import"
			if generated {
				want = "ssh_key.generate"
			}
			if !strings.Contains(actions, want) || !strings.Contains(actions, "ssh_key.reveal") || !strings.Contains(actions, "ssh_key.delete") {
				t.Fatal(actions)
			}
			if strings.Contains(detail, input.PrivateKey) || strings.Contains(detail, input.PublicKey) || !strings.Contains(detail, input.Name) {
				t.Fatal("invalid audit redaction")
			}
			if strings.Contains(fmt.Sprintf("%#v", input), input.PrivateKey) {
				t.Fatal("input logging disclosed secret")
			}
		})
	}
}

func TestSSHMutationAuditFailurePreservesKeyAndBlocksReveal(t *testing.T) {
	f := newSSHMutationFixture(t)
	key, err := f.keys.CreateSSHKey(t.Context(), sshMutationInput())
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, "CREATE TRIGGER reject_ssh_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'fixture audit unavailable'); END")
	if err := f.keys.DeleteSSHKey(f.ctx, key.ID); err == nil {
		t.Fatal("delete accepted without audit")
	}
	if private, err := f.keys.ReadPrivateKey(f.ctx, key.ID); err == nil || private != "" {
		t.Fatal("reveal returned private key without audit")
	}
	input := sshMutationInput()
	input.Name = "New SSH key"
	if _, err := f.keys.CreateSSHKey(f.ctx, input); err == nil {
		t.Fatal("create accepted without audit")
	}
	keys, err := f.keys.ListSSHKeys(t.Context())
	if err != nil || len(keys) != 1 || keys[0].ID != key.ID {
		t.Fatal("audit failure changed key inventory", err)
	}
	if private, err := f.keys.ReadPrivateKey(t.Context(), key.ID); err != nil || private != sshMutationInput().PrivateKey {
		t.Fatal("failed delete damaged ciphertext", err)
	}
}

func TestSSHMutationRechecksCurrentAuthority(t *testing.T) {
	for _, statement := range []string{"UPDATE admin_sessions SET reauthenticated_until=NULL", "UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP", "UPDATE admin_users SET enabled=0"} {
		t.Run(statement, func(t *testing.T) {
			f := newSSHMutationFixture(t)
			key, err := f.keys.CreateSSHKey(t.Context(), sshMutationInput())
			if err != nil {
				t.Fatal(err)
			}
			f.exec(t, statement)
			if _, err := f.keys.CreateSSHKey(f.ctx, SSHKeyInput{Name: "Other", PublicKey: "other"}); err == nil {
				t.Fatal("stale principal created credentials")
			}
			if err := f.keys.DeleteSSHKey(f.ctx, key.ID); err == nil {
				t.Fatal("stale principal deleted credentials")
			}
			if private, err := f.keys.ReadPrivateKey(f.ctx, key.ID); err == nil || private != "" {
				t.Fatal("stale principal revealed credentials")
			}
		})
	}
}

func TestSSHMutationAdminAndBootstrapAuthority(t *testing.T) {
	f := newSSHMutationFixture(t)
	actor := f.addActor(t, "operator", model.AdminRoleAdmin)
	ctx := WithMetadataActor(t.Context(), MetadataActor{DashboardActor: actor})
	if _, err := f.keys.CreateSSHKey(ctx, sshMutationInput()); err != nil {
		t.Fatal("recent admin must be allowed", err)
	}
	ctx = WithMetadataActor(t.Context(), MetadataActor{Bootstrap: true})
	if _, err := f.keys.CreateSSHKey(ctx, SSHKeyInput{Name: "Bootstrap", PublicKey: "other"}); !errors.Is(err, ErrMetadataForbidden) {
		t.Fatal("bootstrap issued credentials", err)
	}
}

func TestSSHMutationWriteAndSnapshotFaults(t *testing.T) {
	f := newSSHMutationFixture(t)
	key, err := f.keys.CreateSSHKey(t.Context(), sshMutationInput())
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, "CREATE TRIGGER ignore_ssh_delete BEFORE DELETE ON ssh_keys BEGIN SELECT RAISE(IGNORE); END")
	if err := f.keys.DeleteSSHKey(f.ctx, key.ID); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal("ignored deletion was reported successful", err)
	}
	if err := f.keys.authorizeSSHKeyRead(f.ctx, key.ID, []byte("different ciphertext")); !errors.Is(err, ErrAdminMutationConflict) {
		t.Fatal("changed ciphertext accepted", err)
	}
	f.exec(t, "CREATE TRIGGER expire_ssh_proof AFTER INSERT ON ssh_keys BEGIN UPDATE admin_sessions SET reauthenticated_until=NULL; END")
	if _, err := f.keys.CreateSSHKey(f.ctx, SSHKeyInput{Name: "Raced", PublicKey: "other"}); !errors.Is(err, ErrMetadataRecentAuth) {
		t.Fatal("in-transaction proof change accepted", err)
	}
	keys, err := f.keys.ListSSHKeys(t.Context())
	if err != nil || len(keys) != 1 {
		t.Fatal("failed mutation committed", err)
	}
}
