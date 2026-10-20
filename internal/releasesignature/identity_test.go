package releasesignature

import (
	"errors"
	"testing"
)

func TestReleaseTagValidation(t *testing.T) {
	for _, tag := range []string{"v0.0.0", "v2.10.3", "v0.2.0-dev.14+build.007", "v1.2.3-alpha.0", "v1.2.3-alpha.01x"} {
		if !ValidReleaseTag(tag) {
			t.Errorf("valid release tag rejected: %q", tag)
		}
	}
	for _, tag := range []string{"", "2.0.0", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-01", "v1.2.3-alpha.01", "v1.2.3/../other", "v1.2.3\n", "v1.2", "v1.2.3+", "v1.2.3-a..b"} {
		if ValidReleaseTag(tag) {
			t.Errorf("invalid release tag accepted: %q", tag)
		}
	}
}

func TestTaggedVerifierUsesExactVersionBoundIdentity(t *testing.T) {
	config := verifierFixture(t, `case "$9" in
https://github.com/beat-org/beat/.github/workflows/release.yml@refs/tags/v2.0.0) exit 0;;
https://github.com/beat-org/beat/.github/workflows/release.yml@refs/tags/v2.1.0-preview.1) exit 0;;
*) exit 9;;
esac`)
	config.CertificateIdentity = ""
	config.ReleaseWorkflow = "https://github.com/beat-org/beat/.github/workflows/release.yml"
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"v2.0.0", "v2.1.0-preview.1"} {
		evidence := testEvidence()
		evidence.ReleaseTag = tag
		if err := v.Verify(t.Context(), evidence); err != nil {
			t.Fatal("expected exact workflow and version binding", err)
		}
	}
	if err := v.Verify(t.Context(), testEvidence()); !errors.Is(err, ErrRejected) {
		t.Fatal("missing tag accepted", err)
	}
	evidence := testEvidence()
	evidence.ReleaseTag = "v1.2.3/other"
	if err := v.Verify(t.Context(), evidence); !errors.Is(err, ErrRejected) {
		t.Fatal("identity escape accepted", err)
	}
	assertVerifierClean(t, config.TemporaryDirectory)
}

func TestWorkflowPolicyCannotBeAnUploadedIdentityPattern(t *testing.T) {
	for _, workflow := range []string{"https://github.com/owner/repo/.github/workflows/release.yml.*", "https://github.com/owner/repo/.github/workflows/release.yml@refs/tags/v1.0.0", "http://github.com/owner/repo/.github/workflows/release.yml", "https://github.com/owner/../.github/workflows/release.yml", "https://github.com.evil/owner/repo/.github/workflows/release.yml"} {
		config := verifierFixture(t, "exit 0")
		config.CertificateIdentity = ""
		config.ReleaseWorkflow = workflow
		if _, err := New(config); !errors.Is(err, ErrConfiguration) {
			t.Fatal("invalid workflow policy accepted", err)
		}
	}
	config := verifierFixture(t, "exit 0")
	config.ReleaseWorkflow = "https://github.com/owner/repo/.github/workflows/release.yaml"
	if _, err := New(config); !errors.Is(err, ErrConfiguration) {
		t.Fatal("ambiguous identity policies accepted", err)
	}
}
