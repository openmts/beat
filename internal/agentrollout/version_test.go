package agentrollout

import (
	"testing"

	"github.com/beat/backend/internal/releasesignature"
)

func TestSemanticVersionAndSignatureTagContract(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "10.0.0"}
	for index, version := range ordered {
		tag, err := ReleaseTag(version)
		if err != nil || tag != "v"+version || !releasesignature.ValidReleaseTag(tag) {
			t.Fatal(version, tag, err)
		}
		if equal, ok := compareVersion("v"+version, version); !ok || equal != 0 {
			t.Fatal("v prefix changed precedence")
		}
		if index == 0 {
			continue
		}
		greater, ok := compareVersion(version, ordered[index-1])
		less, reverseOK := compareVersion(ordered[index-1], version)
		if !ok || !reverseOK || greater != 1 || less != -1 {
			t.Fatal(version, greater, less)
		}
	}
	for _, version := range []string{"", "v", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-", "1.2.3-a..b", "1.2.3+x+y", "1.2.3+", "1.2.3-evil/path", "1.2.3+_"} {
		if _, err := ReleaseTag(version); err == nil {
			t.Fatal("invalid version mapped to certificate identity", version)
		}
	}
	if order, ok := compareVersion("1.2.3+build.1", "1.2.3+build.2"); !ok || order != 0 {
		t.Fatal("build metadata changed precedence")
	}
	if tag, err := ReleaseTag("v1.2.3+build.1"); err != nil || tag != "v1.2.3+build.1" {
		t.Fatal(tag, err)
	}
}
