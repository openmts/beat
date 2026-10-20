package protocol

import (
	"strings"
	"testing"
)

func TestPolicyHardLimitsAndIntersection(t *testing.T) {
	for _, mutate := range []func(*Policy){
		func(p *Policy) { p.MaximumTargets = 0 }, func(p *Policy) { p.MaximumTargets = 201 }, func(p *Policy) { p.MaximumActiveTasks = 0 },
		func(p *Policy) { p.Concurrency = 0 }, func(p *Policy) { p.Concurrency = 5 }, func(p *Policy) { p.TimeoutSeconds = 0 }, func(p *Policy) { p.TimeoutSeconds = 3601 },
		func(p *Policy) { p.OutputBytes = 0 }, func(p *Policy) { p.OutputBytes = MaximumOutputBytes + 1 }, func(p *Policy) { p.Kinds = []string{"unknown"} },
		func(p *Policy) { p.Kinds = []string{"shell", "shell"} }, func(p *Policy) { p.Roots = []Root{{ID: "../bad", MaximumBytes: 1}} },
		func(p *Policy) { p.Roots = []Root{{ID: "inbox", MaximumBytes: 0}} }, func(p *Policy) { p.Roots = []Root{{ID: "inbox", MaximumBytes: MaximumFileBytes + 1}} },
		func(p *Policy) { p.Roots = []Root{{ID: "inbox", MaximumBytes: 1}, {ID: "inbox", MaximumBytes: 1}} },
	} {
		policy := testPolicy()
		mutate(&policy)
		if policy.Validate() != ErrInvalid {
			t.Fatalf("invalid policy accepted: %+v", policy)
		}
	}
	policy := testPolicy()
	policy.Roots = []Root{{ID: "inbox", MaximumBytes: 1}}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	limits := Limits{Kind: "shell", Targets: 1, TimeoutSeconds: 30, OutputBytes: 1024}
	if err := Authorize(policy, policy, limits); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Limits){func(l *Limits) { l.Kind = "collect" }, func(l *Limits) { l.Targets = 0 }, func(l *Limits) { l.Targets = 51 },
		func(l *Limits) { l.TimeoutSeconds = 301 }, func(l *Limits) { l.TimeoutSeconds = 0 }, func(l *Limits) { l.OutputBytes = 0 }, func(l *Limits) { l.OutputBytes = MaximumOutputBytes }, func(l *Limits) { l.RunAsRoot = true }} {
		changed := limits
		mutate(&changed)
		if Authorize(policy, policy, changed) != ErrForbidden {
			t.Fatal("limits expanded")
		}
	}
	local := policy
	local.Enabled = false
	if Authorize(policy, local, limits) != ErrForbidden {
		t.Fatal("disabled accepted")
	}
	local = Policy{}
	if Authorize(policy, local, limits) != ErrForbidden {
		t.Fatal("corrupt policy accepted")
	}
}

func TestCommandAndPathValidation(t *testing.T) {
	for _, command := range [][]byte{nil, []byte{255}, []byte("x\x00y"), make([]byte, MaximumCommandBytes+1)} {
		if ValidateCommand(command) != ErrInvalid {
			t.Fatal("invalid command accepted")
		}
	}
	if ValidateCommand([]byte("echo ok")) != nil {
		t.Fatal("valid command rejected")
	}
	for _, path := range []string{"", "/etc/passwd", "../file", "a/../file", "a/./b", "a//b", "a/", "C:foo", "a\\b", "a\x00b", "a\x01b", "a\x7fb", "a. ", "CON.txt", "LPT1", strings.Repeat("a", 256), strings.Repeat("a/", MaximumPathComponents) + "a", string([]byte{255}), strings.Repeat("a", MaximumPathBytes+1)} {
		if ValidateRelativePath(path) != ErrInvalid {
			t.Fatalf("invalid path accepted: %q", path)
		}
	}
	if ValidateRelativePath("nested/文件.txt") != nil {
		t.Fatal("valid relative path rejected")
	}
	for _, id := range []string{"", strings.Repeat("x", 129), "bad/id"} {
		if ValidID(id) {
			t.Fatal("invalid root id")
		}
	}
}

func testPolicy() Policy {
	return Policy{Enabled: true, Kinds: []string{"shell"}, MaximumTargets: 50, MaximumActiveTasks: 5, Concurrency: 1, TimeoutSeconds: 300, OutputBytes: 256 * 1024}
}
