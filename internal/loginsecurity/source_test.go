package loginsecurity

import (
	"bytes"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestSummarizeSourceKeepsOnlyTruncatedPrefix(t *testing.T) {
	cases := []struct {
		raw    string
		class  string
		family string
		prefix string
	}{
		{"203.0.113.44", model.SecuritySourceClassPublic, model.SecuritySourceFamilyIPv4, "203.0.113.0/24"},
		{"198.51.100.9:51234", model.SecuritySourceClassPublic, model.SecuritySourceFamilyIPv4, "198.51.100.0/24"},
		{"10.1.2.3", model.SecuritySourceClassPrivate, model.SecuritySourceFamilyIPv4, "private"},
		{"127.0.0.1", model.SecuritySourceClassLoopback, model.SecuritySourceFamilyIPv4, "loopback"},
		{"169.254.10.4", model.SecuritySourceClassLinkLocal, model.SecuritySourceFamilyIPv4, "link_local"},
		{"[2001:db8:abcd:12::9]:443", model.SecuritySourceClassPublic, model.SecuritySourceFamilyIPv6, "2001:db8:abcd::/48"},
		{"fd00::1", model.SecuritySourceClassPrivate, model.SecuritySourceFamilyIPv6, "unique_local"},
		{"fe80::1%eth0", model.SecuritySourceClassLinkLocal, model.SecuritySourceFamilyIPv6, "link_local"},
		{"", model.SecuritySourceClassUnknown, model.SecuritySourceFamilyUnknown, "unknown"},
		{"not-an-address", model.SecuritySourceClassUnknown, model.SecuritySourceFamilyUnknown, "unknown"},
	}
	for _, testCase := range cases {
		source := SummarizeSource(testCase.raw)
		if source.Class != testCase.class || source.Family != testCase.family || source.Prefix != testCase.prefix {
			t.Fatalf("SummarizeSource(%q) = %+v, want %s/%s/%s", testCase.raw,
				source, testCase.class, testCase.family, testCase.prefix)
		}
		if source.Summary() == "" {
			t.Fatalf("SummarizeSource(%q) produced an empty summary", testCase.raw)
		}
	}
}

func TestSummarizeDeviceNormalizesFamilies(t *testing.T) {
	cases := []struct {
		raw     string
		browser string
		os      string
		class   string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36",
			"chrome", "windows", model.SecurityDeviceClassDesktop},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) Version/17.0 Mobile/15E148 Safari/604.1",
			"safari", "ios", model.SecurityDeviceClassMobile},
		{"Mozilla/5.0 (iPad; CPU OS 17_1 like Mac OS X) Version/17.0 Mobile/15E148 Safari/604.1",
			"safari", "ipados", model.SecurityDeviceClassTablet},
		{"curl/8.4.0", "curl", "", model.SecurityDeviceClassAutomation},
		{"Go-http-client/1.1", "go_http_client", "", model.SecurityDeviceClassAutomation},
		{"", "unknown", "unknown", model.SecurityDeviceClassUnknown},
	}
	for _, testCase := range cases {
		device := SummarizeDevice(testCase.raw)
		if device.Browser != testCase.browser || device.Class != testCase.class {
			t.Fatalf("SummarizeDevice(%q) = %+v, want %s/%s", testCase.raw, device, testCase.browser, testCase.class)
		}
		if testCase.os != "" && device.OS != testCase.os {
			t.Fatalf("SummarizeDevice(%q) OS = %s, want %s", testCase.raw, device.OS, testCase.os)
		}
	}
	long := "Mozilla/5.0 " + string(bytes.Repeat([]byte("x"), 4096))
	if device := SummarizeDevice(long); device.Class == "" {
		t.Fatal("oversized user agent produced no device class")
	}
}

func TestFingerprintIsKeyedAndEpochScoped(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	first := Fingerprint(key, "epoch-1", "203.0.113.0/24")
	if len(first) != 32 {
		t.Fatalf("fingerprint length = %d", len(first))
	}
	if !bytes.Equal(first, Fingerprint(key, "epoch-1", "203.0.113.0/24")) {
		t.Fatal("fingerprint is not deterministic")
	}
	if bytes.Equal(first, Fingerprint(key, "epoch-2", "203.0.113.0/24")) {
		t.Fatal("fingerprint ignored the epoch")
	}
	if bytes.Equal(first, Fingerprint([]byte("another-key-another-key-another"), "epoch-1", "203.0.113.0/24")) {
		t.Fatal("fingerprint ignored the key")
	}
	if bytes.Equal(first, Fingerprint(key, "epoch-1", "203.0.114.0/24")) {
		t.Fatal("fingerprint ignored the value")
	}
}
