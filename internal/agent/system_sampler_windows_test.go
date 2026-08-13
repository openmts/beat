//go:build windows

package agent

import "testing"

func TestIsSubpathWindows(t *testing.T) {
	tests := []struct {
		candidate string
		parent    string
		want      bool
	}{
		{`C:\`, `C:\`, true},
		{`C:\Users`, `C:\`, true},
		{`C:\Users\Data`, `C:\Users`, true},
		{`C:\`, `C:\Users`, false},
		{`D:\data`, `C:\`, false},
		{`C:\Users2`, `C:\Users`, false},
		{`\\server\share`, `\\server`, true},
	}
	for _, test := range tests {
		if got := isSubpath(test.candidate, test.parent); got != test.want {
			t.Fatalf("isSubpath(%q, %q) = %v, want %v", test.candidate, test.parent, got, test.want)
		}
	}
}
