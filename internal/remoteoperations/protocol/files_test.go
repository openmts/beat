package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func validFile() FileSpec {
	digest := sha256.Sum256([]byte("x"))
	return FileSpec{RootID: "inbox", RelativePath: "a/file.txt", Size: 1, Digest: hex.EncodeToString(digest[:]), Mode: 0o600}
}

func filePolicy() Policy {
	policy := testPolicy()
	policy.Kinds = []string{"deploy", "collect"}
	policy.Roots = []Root{{ID: "inbox", Name: "Application files", Read: true, Write: true, MaximumBytes: 10}}
	return policy
}

func TestFilePolicyIntersectionRejectsExpandedCapabilities(t *testing.T) {
	policy := filePolicy()
	request := FileRequest{Direction: "deploy", File: validFile()}
	if err := AuthorizeFile(policy, policy, request); err != nil {
		t.Fatal(err)
	}
	request.Direction = "collect"
	if err := AuthorizeFile(policy, policy, request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Policy){func(p *Policy) { p.Enabled = false }, func(p *Policy) { p.Kinds = nil }, func(p *Policy) { p.Roots = nil }, func(p *Policy) { p.Roots[0].Read = false }, func(p *Policy) { p.OutputBytes = 0 }} {
		local := filePolicy()
		mutate(&local)
		if AuthorizeFile(policy, local, request) != ErrForbidden {
			t.Fatal("local read restriction bypassed")
		}
	}
	request.Direction = "deploy"
	local := filePolicy()
	local.Roots[0].Write = false
	if AuthorizeFile(policy, local, request) != ErrForbidden {
		t.Fatal("write denied")
	}
	request.File.Overwrite = true
	if AuthorizeFile(policy, policy, request) != ErrForbidden {
		t.Fatal("overwrite expanded")
	}
	request.File.Overwrite = false
	request.File.Mode = 0o700
	if AuthorizeFile(policy, policy, request) != ErrForbidden {
		t.Fatal("executable expanded")
	}
	request.File.Mode = 0o600
	request.File.Size = 11
	if AuthorizeFile(policy, policy, request) != ErrForbidden {
		t.Fatal("root quota bypassed")
	}
	request.Direction = "unknown"
	if AuthorizeFile(policy, policy, request) != ErrForbidden {
		t.Fatal("unknown direction")
	}
}

func TestFilePathDigestAndModeLimits(t *testing.T) {
	for _, mutate := range []func(*FileSpec){func(f *FileSpec) { f.RootID = "" }, func(f *FileSpec) { f.RelativePath = "../escape" }, func(f *FileSpec) { f.Size = -1 },
		func(f *FileSpec) { f.Size = MaximumFileBytes + 1 }, func(f *FileSpec) { f.Mode = 0o644 }, func(f *FileSpec) { f.Digest = "invalid" }} {
		file := validFile()
		mutate(&file)
		if ValidateFile(file) != ErrInvalid {
			t.Fatal("invalid file accepted")
		}
	}
}

func TestShellAndRetentionPolicyBounds(t *testing.T) {
	policy := testPolicy()
	policy.SupportedShells = []string{"sh"}
	if AuthorizeShell(policy, policy, "sh") != nil || AuthorizeShell(policy, policy, "powershell") != ErrForbidden {
		t.Fatal("shell family intersection")
	}
	local := policy
	local.SupportedShells = []string{"powershell"}
	if AuthorizeShell(policy, local, "powershell") != ErrForbidden {
		t.Fatal("owner shell restriction")
	}
	policy.Retention = &RetentionPolicy{TaskDays: 1}
	if policy.RetentionLimits().OutputDays != 1 || policy.Validate() != nil {
		t.Fatal("retention defaults")
	}
	for _, retention := range []RetentionPolicy{{TaskDays: 91}, {TaskDays: 2, OutputDays: 3}, {ArtifactHours: 25}, {QueueHours: 25}, {UploadHours: 7}} {
		policy.Retention = &retention
		if policy.Validate() != ErrInvalid {
			t.Fatal("unbounded retention")
		}
	}
	policy.Retention = nil
	for _, shells := range [][]string{{"sh", "sh"}, {"bad"}, {"sh", "powershell", "sh"}} {
		policy.SupportedShells = shells
		if policy.Validate() != ErrInvalid {
			t.Fatal("invalid shell declaration")
		}
	}
	policy = filePolicy()
	policy.Roots[0].Name = "bad\x00name"
	if policy.Validate() != ErrInvalid {
		t.Fatal("control character in display name")
	}
}
