package nodelifecycle

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestLifecycleStorageFailuresRollback(t *testing.T) {
	for _, stage := range []string{"node", "audit", "event", "routing", "settings"} {
		t.Run(stage, func(t *testing.T) {
			f := lifecycleFixture(t)
			input := validWrite()
			input.ExpiresOn = "2026-02-28"
			f.save(t, input)
			triggers := map[string]string{
				"node":     `CREATE TRIGGER fail BEFORE UPDATE ON node_lifecycle BEGIN SELECT RAISE(ABORT,'fail');END`,
				"audit":    `CREATE TRIGGER fail BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'fail');END`,
				"event":    `CREATE TRIGGER fail BEFORE INSERT ON node_lifecycle_events BEGIN SELECT RAISE(ABORT,'fail');END`,
				"routing":  `CREATE TRIGGER fail BEFORE UPDATE ON node_lifecycle_events BEGIN SELECT RAISE(ABORT,'fail');END`,
				"settings": `CREATE TRIGGER fail BEFORE UPDATE ON lifecycle_settings BEGIN SELECT RAISE(ABORT,'fail');END`,
			}
			f.exec(t, triggers[stage])
			var err error
			if stage == "settings" {
				_, err = f.service.SaveSettings(t.Context(), f.actor, DefaultSettings())
			} else {
				input.ExpiresOn = "2026-03-31"
				_, err = f.service.SaveNode(t.Context(), f.actor, NodeChange{NodeID: "life-node", LifecycleWrite: input})
			}
			if err == nil {
				t.Fatal("fault mutation succeeded")
			}
			value, err := f.service.Node(t.Context(), f.actor, "life-node")
			if err != nil || value.ExpiresOn != "2026-02-28" {
				t.Fatal(value, err)
			}
		})
	}
}

func TestLifecycleInvalidAndMissingMetadata(t *testing.T) {
	f := lifecycleFixture(t)
	if _, err := f.service.SaveNode(t.Context(), f.actor, NodeChange{}); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := f.service.SaveNode(t.Context(), f.actor, NodeChange{NodeID: "missing", LifecycleWrite: validWrite()}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.LifecycleSettings{}); err != ErrInvalid {
		t.Fatal(err)
	}
	if value, err := f.service.PublicLifecycle(t.Context(), "missing"); err != nil || value != nil {
		t.Fatal(value, err)
	}
	if value, err := f.service.PublicLifecycle(t.Context(), "life-node"); err != nil || value != nil {
		t.Fatal(value, err)
	}
	f.exec(t, `UPDATE nodes SET is_public=0 WHERE id='life-node'`)
	if value, err := f.service.PublicLifecycle(t.Context(), "life-node"); err != nil || value != nil {
		t.Fatal(value, err)
	}
	if err := f.service.remind(t.Context(), "missing"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.advance(t.Context(), "missing"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.PublicLifecycle(t.Context(), "life-node"); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.service.Tick(t.Context()); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.service.Ready(t.Context()); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestLifecycleMalformedPersistentSettingsFailClosed(t *testing.T) {
	f := lifecycleFixture(t)
	for _, query := range []string{`UPDATE lifecycle_settings SET lead_days='bad'`, `UPDATE lifecycle_settings SET lead_days='[]',post_days='bad'`, `UPDATE lifecycle_settings SET lead_days='[]',post_days='[]',timezone='bad/zone'`} {
		f.exec(t, query)
		if _, err := f.service.Settings(t.Context(), f.actor); err == nil {
			t.Fatal("bad state accepted")
		}
	}
	f.exec(t, `UPDATE lifecycle_settings SET lead_days='null',post_days='null',timezone='UTC'`)
	if settings, err := f.service.Settings(t.Context(), f.actor); err != nil || settings.LeadDays == nil || settings.PostDays == nil {
		t.Fatal(settings, err)
	}
}
