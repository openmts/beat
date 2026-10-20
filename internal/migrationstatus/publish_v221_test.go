package migrationstatus

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
)

// 把 RLIMIT_FSIZE 压到 0 可以让临时标记文件的写入以 EFBIG 失败，用来证明发布阶段
// 的 I/O 失败会被上报，而不是留下半个标记或静默成功。上限必须在断言之前恢复，
// 否则测试进程自身退出时的覆盖率写入会被同一条限制打断。
func TestPublishReportsWriteFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RLIMIT_FSIZE fault injection requires Linux")
	}
	directory := privateTemp(t)
	original := syscall.Rlimit{}
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Skipf("RLIMIT_FSIZE is unavailable: %v", err)
	}
	limited := original
	limited.Cur = 0
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Skipf("cannot lower RLIMIT_FSIZE: %v", err)
	}
	// 写入越界会同时抛 SIGXFSZ；忽略它，让 Write 返回 EFBIG。
	signalIgnoreSIGXFSZ()
	err := publish(directory, []byte(`{"state":"idle"}`))
	if restoreErr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); restoreErr != nil {
		t.Fatalf("restore RLIMIT_FSIZE: %v", restoreErr)
	}
	if !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("expected the write failure to be reported, got %v", err)
	}
	if entries, readErr := os.ReadDir(directory); readErr != nil || len(entries) != 0 {
		t.Fatalf("failed publish left files behind: %v %v", entries, readErr)
	}
}
