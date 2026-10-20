//go:build linux

package agentoperations

import (
	"encoding/json"
	"io"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const helperFailure = 126

// HandleHelper 必须在 Agent 主入口解析配置或开启网络前调用。只处理本机继承的描述符。
func HandleHelper(args []string) (bool, int) {
	if len(args) != 1 {
		return false, 0
	}
	switch args[0] {
	case helperArgument:
		if err := executeHelper(); err != nil {
			return true, helperFailure
		}
		return true, 0
	case guardianArgument:
		return true, executeGuardian()
	default:
		return false, 0
	}
}

func executeHelper() error {
	configuration := os.NewFile(3, "operation-configuration")
	gate := os.NewFile(4, "operation-start-gate")
	var config helperConfig
	decoder := json.NewDecoder(io.LimitReader(configuration, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return ErrBoundary
	}
	if err := configuration.Close(); err != nil {
		return ErrBoundary
	}
	var authorization [1]byte
	if _, err := io.ReadFull(gate, authorization[:]); err != nil || authorization[0] != 1 {
		return ErrBoundary
	}
	if err := gate.Close(); err != nil {
		return ErrBoundary
	}
	return execRestrictedShell(config)
}

func execRestrictedShell(config helperConfig) error {
	if !validHelperLimits(config) {
		return ErrBoundary
	}
	runtime.LockOSThread()
	if err := unix.Chdir(config.Working); err != nil {
		return ErrBoundary
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return ErrBoundary
	}
	if err := setExecutionLimits(config); err != nil {
		return err
	}
	if err := setHelperIdentity(config); err != nil {
		return err
	}
	environment := []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "HOME=" + config.Working}
	return execWithAddressLimit(config, environment)
}

func setExecutionLimits(config helperConfig) error {
	limits := []struct {
		resource int
		value    uint64
	}{
		{unix.RLIMIT_CORE, 0}, {unix.RLIMIT_NOFILE, 64}, {unix.RLIMIT_NPROC, config.Processes},
		{unix.RLIMIT_CPU, uint64(config.TimeoutSeconds + 2)}, {unix.RLIMIT_FSIZE, uint64(config.OutputBytes)},
	}
	for _, limit := range limits {
		if err := unix.Setrlimit(limit.resource, &unix.Rlimit{Cur: limit.value, Max: limit.value}); err != nil {
			return ErrBoundary
		}
	}
	return nil
}

func executeGuardian() int {
	parent := os.NewFile(3, "operation-parent-lifetime")
	kill := os.NewFile(4, "operation-cgroup-kill")
	var content [1]byte
	_, readErr := parent.Read(content[:])
	_, killErr := kill.WriteString("1")
	closeErr := kill.Close()
	_ = parent.Close()
	if readErr != nil && readErr != io.EOF {
		return helperFailure
	}
	if killErr != nil || closeErr != nil {
		return helperFailure
	}
	return 0
}

func execWithAddressLimit(config helperConfig, environment []string) error {
	shell, err := unix.BytePtrFromString(config.Shell)
	if err != nil {
		return ErrBoundary
	}
	arguments, err := syscall.SlicePtrFromStrings([]string{config.Shell, "-s"})
	if err != nil {
		return ErrBoundary
	}
	variables, err := syscall.SlicePtrFromStrings(environment)
	if err != nil {
		return ErrBoundary
	}
	limit := config.MemoryBytes * 2
	if err := unix.Setrlimit(unix.RLIMIT_AS, &unix.Rlimit{Cur: limit, Max: limit}); err != nil {
		return ErrBoundary
	}
	_, _, executeErr := unix.RawSyscall(unix.SYS_EXECVE, uintptr(unsafe.Pointer(shell)), uintptr(unsafe.Pointer(&arguments[0])), uintptr(unsafe.Pointer(&variables[0])))
	runtime.KeepAlive(shell)
	runtime.KeepAlive(arguments)
	runtime.KeepAlive(variables)
	if executeErr != 0 {
		return ErrBoundary
	}
	return nil
}

func validHelperLimits(config helperConfig) bool {
	return config.TimeoutSeconds >= 1 && config.TimeoutSeconds <= 3600 && config.OutputBytes >= 1 && config.OutputBytes <= 1<<20 && config.MemoryBytes >= 16<<20 && config.MemoryBytes <= 4<<30 && config.Processes >= 1 && config.Processes <= 1024
}

func setHelperIdentity(config helperConfig) error {
	if os.Geteuid() == 0 {
		if err := unix.Setgroups([]int{}); err != nil {
			return ErrBoundary
		}
		if err := unix.Setgid(int(config.GID)); err != nil {
			return ErrBoundary
		}
		if err := unix.Setuid(int(config.UID)); err != nil {
			return ErrBoundary
		}
	}
	if uint32(os.Geteuid()) != config.UID {
		return ErrBoundary
	}
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0); err != nil {
		return ErrBoundary
	}
	if os.Getppid() == 1 {
		return ErrBoundary
	}
	return nil
}
