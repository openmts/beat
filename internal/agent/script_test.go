package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecuteScript(t *testing.T) {
	result := ExecuteScript(context.Background(), "printf 'hello task'", 5*time.Second)
	if result.Status != "success" || result.ExitCode != 0 || result.Output != "hello task" {
		t.Fatalf("unexpected script result: %+v", result)
	}

	result = ExecuteScript(context.Background(), "printf 'failed output'; exit 7", 5*time.Second)
	if result.Status != "failed" || result.ExitCode != 7 || !strings.Contains(result.Output, "failed output") {
		t.Fatalf("unexpected failed script result: %+v", result)
	}

	result = ExecuteScript(context.Background(), "sleep 5", 50*time.Millisecond)
	if result.Status != "timeout" || result.DurationMs >= 2000 {
		t.Fatalf("unexpected timeout script result: %+v", result)
	}
}
