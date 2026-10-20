//go:build !linux && !windows

package agentoperations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

const unsupportedHelperFailure = 126

// 非Linux/Windows平台只支持监控；没有经过验收的执行与文件边界。
func operationPlatformSupported() bool { return false }

func defaultShell() string { return "" }

func shellSupported(string) bool { return false }

func supportedShells() []string { return []string{} }

func resolveIdentity(CommandConfig) (identity, error) { return identity{}, ErrBoundary }

func validateBoundary(settings) error { return ErrBoundary }

func (*engine) Recover(string) error { return ErrBoundary }

func (*engine) execute(context.Context, execution, *outputCapture) (int, error) {
	return -1, ErrBoundary
}

func HandleHelper(args []string) (bool, int) {
	if len(args) == 1 && (args[0] == helperArgument || args[0] == guardianArgument) {
		return true, unsupportedHelperFailure
	}
	return false, 0
}

func splitPath(path string) []string { return strings.Split(path, string(filepath.Separator)) }

func platformDeniedRoots() []string { return []string{"/"} }

func privateDirectory(string, bool) error { return ErrBoundary }

func privateFile(string) error { return ErrBoundary }

func lockState(string) (func() error, error) { return nil, ErrBoundary }

func replacePrivate(string, string) error { return ErrBoundary }

func syncDirectory(string) error { return ErrBoundary }

func secureCreatedDirectory(string) error { return ErrBoundary }

func validateOperationDirectory(string) error { return ErrBoundary }

func checkSensitiveFile(string) error { return ErrBoundary }

func secureCreatedFile(*os.File) error { return ErrBoundary }
