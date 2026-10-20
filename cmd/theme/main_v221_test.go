package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// main 只在真实进程里执行；同包覆盖 main 的标准做法是用自身二进制跑一次子进程，
// 并把父进程的 GOCOVERDIR 透传下去，让子进程的计数落进同一次覆盖率统计。
func TestMainExitsWithFailureCodeOnUnknownFlag(t *testing.T) {
	if os.Getenv("BEAT_THEME_MAIN_CHILD") == "1" {
		main()
		return
	}
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMainExitsWithFailureCodeOnUnknownFlag$")
	child.Env = append(os.Environ(), "BEAT_THEME_MAIN_CHILD=1")
	if directory := os.Getenv("GOCOVERDIR"); directory != "" {
		child.Env = append(child.Env, "GOCOVERDIR="+directory)
	}
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("expected the child to exit with 1, got %v (%s)", err, output)
	}
	if !strings.Contains(string(output), "theme:") {
		t.Fatalf("expected an actionable failure on stderr, got %q", output)
	}
}
