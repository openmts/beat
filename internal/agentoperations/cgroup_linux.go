//go:build linux

package agentoperations

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type linuxBoundary struct {
	pid       int
	directory string
	guardian  *exec.Cmd
	lifetime  *os.File
	once      sync.Once
	err       error
}

func checkCgroupRoot(root string) error {
	if !filepath.IsAbs(root) {
		return ErrBoundary
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return ErrBoundary
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return ErrBoundary
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return ErrBoundary
	}
	return nil
}

func (engine *engine) attachBoundary(id string, pid int) (*linuxBoundary, error) {
	boundary := &linuxBoundary{pid: pid}
	if !requireCgroup(engine.settings.config.Commands) {
		return boundary, nil
	}
	directory := filepath.Join(engine.settings.config.Commands.CgroupRoot, "beat-"+journalName(id))
	if err := os.Mkdir(directory, 0o700); err != nil {
		return nil, ErrBoundary
	}
	boundary.directory = directory
	if err := configureCgroup(directory, engine.settings.config.Commands); err != nil {
		_ = boundary.Close()
		return nil, err
	}
	if err := writeControl(filepath.Join(directory, "cgroup.procs"), strconv.Itoa(pid)); err != nil {
		_ = boundary.Close()
		return nil, ErrBoundary
	}
	if err := boundary.guard(engine.executable); err != nil {
		_ = boundary.Close()
		return nil, err
	}
	return boundary, nil
}

func configureCgroup(directory string, config CommandConfig) error {
	for _, control := range []struct{ name, value string }{
		{"memory.max", strconv.FormatUint(config.MemoryBytes, 10)},
		{"pids.max", strconv.FormatUint(config.MaximumProcesses, 10)},
		{"cpu.max", "100000 100000"},
	} {
		if err := writeControl(filepath.Join(directory, control.name), control.value); err != nil {
			return ErrBoundary
		}
	}
	return nil
}

func writeControl(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(value)
	return errors.Join(writeErr, file.Close())
}

func (boundary *linuxBoundary) guard(executable string) error {
	read, write, err := os.Pipe()
	if err != nil {
		return ErrBoundary
	}
	kill, err := os.OpenFile(filepath.Join(boundary.directory, "cgroup.kill"), os.O_WRONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = read.Close()
		_ = write.Close()
		return ErrBoundary
	}
	command := exec.Command(executable, guardianArgument)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	command.ExtraFiles = []*os.File{read, kill}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	startErr := command.Start()
	_ = read.Close()
	_ = kill.Close()
	if startErr != nil {
		_ = write.Close()
		return ErrBoundary
	}
	boundary.guardian, boundary.lifetime = command, write
	return nil
}

func (boundary *linuxBoundary) Kill() error {
	if boundary.directory == "" {
		return killGroup(boundary.pid)
	}
	return writeControl(filepath.Join(boundary.directory, "cgroup.kill"), "1")
}

func (boundary *linuxBoundary) Close() error {
	boundary.once.Do(func() {
		boundary.err = boundary.Kill()
		if boundary.lifetime != nil {
			boundary.err = errors.Join(boundary.err, boundary.lifetime.Close(), boundary.guardian.Wait())
		}
		if boundary.directory != "" {
			boundary.err = errors.Join(boundary.err, removeEmptyCgroup(boundary.directory))
		}
	})
	return boundary.err
}

func removeEmptyCgroup(directory string) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(filepath.Join(directory, "cgroup.events"))
		if err != nil {
			return ErrBoundary
		}
		if strings.Contains(string(contents), "populated 0") {
			return os.Remove(directory)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("%w: process tree did not terminate", ErrBoundary)
}

func (engine *engine) Recover(id string) error {
	if !requireCgroup(engine.settings.config.Commands) {
		return nil
	}
	directory := filepath.Join(engine.settings.config.Commands.CgroupRoot, "beat-"+journalName(id))
	if _, err := os.Stat(directory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return ErrBoundary
	}
	if err := writeControl(filepath.Join(directory, "cgroup.kill"), "1"); err != nil {
		return ErrBoundary
	}
	return removeEmptyCgroup(directory)
}
