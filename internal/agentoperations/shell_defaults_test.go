package agentoperations

import (
	"strings"
	"testing"
	"unicode"
)

func TestWindowsDefaultShellContainsLiteralVersionSeparator(t *testing.T) {
	want := strings.Join([]string{"C:", "Windows", "System32", "WindowsPowerShell", "v1.0", "powershell.exe"}, "\\")
	if windowsDefaultShell != want {
		t.Fatalf("default shell mismatch: %q", windowsDefaultShell)
	}
	for _, value := range windowsDefaultShell {
		if unicode.IsControl(value) {
			t.Fatalf("default shell contains control character %U", value)
		}
	}
}
