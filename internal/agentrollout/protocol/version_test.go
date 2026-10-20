package protocol

import "testing"

func TestVersionPrecedenceAndSafeReleaseTags(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "10.0.0"}
	for i, version := range ordered {
		if tag, err := ReleaseTag("v" + version); err != nil || tag != "v"+version {
			t.Fatal(tag, err)
		}
		if order, ok := CompareVersions(version, version); !ok || order != 0 {
			t.Fatal(order, ok)
		}
		if i == 0 {
			continue
		}
		if order, ok := CompareVersions(version, ordered[i-1]); !ok || order != 1 {
			t.Fatal(order, ok)
		}
		if order, ok := CompareVersions(ordered[i-1], version); !ok || order != -1 {
			t.Fatal(order, ok)
		}
	}
	for _, value := range []string{"", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-", "1.2.3-a..b", "1.2.3+x+y", "1.2.3+", "1.2.3-evil/path", "1.2.3+_"} {
		if _, err := ReleaseTag(value); err == nil {
			t.Fatal(value)
		}
	}
	if order, ok := CompareVersions("1.2.3+build.1", "1.2.3+build.2"); !ok || order != 0 {
		t.Fatal(order, ok)
	}
	if _, ok := CompareVersions("invalid", "1.0.0"); ok {
		t.Fatal("invalid target")
	}
	if _, ok := CompareVersions("1.0.0", "invalid"); ok {
		t.Fatal("invalid current")
	}
}

func TestSameReleaseVersionNormalizesOnlyTheOptionalPrefix(t *testing.T) {
	if !SameReleaseVersion("v1.2.3+build.1", "1.2.3+build.1") {
		t.Fatal("tag prefix changed release identity")
	}
	for _, pair := range [][2]string{{"dev", "dev"}, {"1.0.0", "dev"}, {"1.2.3+build.1", "1.2.3+build.2"}, {"1.2.3-rc.1", "1.2.3"}} {
		if SameReleaseVersion(pair[0], pair[1]) {
			t.Fatal("different release identities matched", pair)
		}
	}
}
