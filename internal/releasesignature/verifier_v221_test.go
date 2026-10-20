package releasesignature

import (
	"errors"
	"os"
	"testing"
)

// 验证程序摘要必须是可读的有界内容：空文件在启动期直接失败，而不是留到校验时才炸。
func TestNewRejectsEmptyVerifierExecutable(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	if err := os.Truncate(config.Executable, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.Executable, 0o700); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(config.Executable); err != nil || info.Size() != 0 {
		t.Fatalf("fixture must be empty: %v %v", info, err)
	}
	if _, err := New(config); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("expected an empty verifier to be rejected, got %v", err)
	}
}
