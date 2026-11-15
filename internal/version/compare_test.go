package version

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0-dev", "0.1.0", -1},
		{"0.2.0", "0.1.9", 1},
		{"v1.2.3", "1.2.2", 1},
		{"1.2.3+build.1", "1.2.3", 0},
	}
	for _, test := range tests {
		got := Compare(test.left, test.right)
		if got < 0 {
			got = -1
		} else if got > 0 {
			got = 1
		}
		if got != test.want {
			t.Fatalf("Compare(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}
