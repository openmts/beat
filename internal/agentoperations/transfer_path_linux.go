//go:build linux

package agentoperations

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"

	"golang.org/x/sys/unix"
)

type transferRoot struct {
	file   *os.File
	device uint64
}

type transferDestination struct {
	parent *os.File
	name   string
	device uint64
}

func openTransferRoot(directory string) (*transferRoot, error) {
	descriptor, err := unix.Openat2(unix.AT_FDCWD, directory, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, ErrBoundary
	}
	file := os.NewFile(uintptr(descriptor), "operation-file-root")
	var info unix.Stat_t
	if err := unix.Fstat(descriptor, &info); err != nil || info.Mode&0o7777 != 0o700 || info.Uid != uint32(os.Geteuid()) {
		_ = file.Close()
		return nil, ErrPolicy
	}
	if !safeTransferFilesystem(descriptor) {
		_ = file.Close()
		return nil, ErrBoundary
	}
	return &transferRoot{file: file, device: uint64(info.Dev)}, nil
}

func (root *transferRoot) Close() error { return root.file.Close() }

func (root *transferRoot) openSource(relative string) (*os.File, error) {
	return openCheckedTransfer(root.file, relative, root.device)
}

func (root *transferRoot) destination(relative string) (*transferDestination, error) {
	parent, err := openRelative(root.file, path.Dir(relative), unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	var info unix.Stat_t
	if err := unix.Fstat(int(parent.Fd()), &info); err != nil || info.Uid != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
		_ = parent.Close()
		return nil, ErrPolicy
	}
	return &transferDestination{parent: parent, name: path.Base(relative), device: root.device}, nil
}

func (destination *transferDestination) Close() error { return destination.parent.Close() }

func openRelative(root *os.File, relative string, flags int) (*os.File, error) {
	how := unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV}
	if flags&unix.O_CREAT != 0 {
		how.Mode = 0o600
	}
	descriptor, err := unix.Openat2(int(root.Fd()), relative, &how)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), "operation-file"), nil
}

func validateTransferFile(file *os.File, device uint64) error {
	var info unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &info); err != nil {
		return ErrStorage
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || uint64(info.Dev) != device || info.Nlink != 1 || info.Uid != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
		return ErrPolicy
	}
	return nil
}

func (destination *transferDestination) temporary(name string, create bool) (*os.File, error) {
	if !validTemporaryName(name) {
		return nil, ErrStorage
	}
	flags := unix.O_RDWR | unix.O_NONBLOCK
	if create {
		flags |= unix.O_CREAT | unix.O_EXCL
	}
	file, err := openRelative(destination.parent, name, flags)
	if err != nil {
		return nil, err
	}
	if err := validateTransferFile(file, destination.device); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (destination *transferDestination) current() (*os.File, error) {
	return openCheckedTransfer(destination.parent, destination.name, destination.device)
}

func (destination *transferDestination) publish(name string, overwrite bool) error {
	if !validTemporaryName(name) {
		return ErrStorage
	}
	existing, err := destination.current()
	if err == nil {
		_ = existing.Close()
		if !overwrite {
			return ErrFileConflict
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	flags := uint(unix.RENAME_NOREPLACE)
	if overwrite {
		flags = 0
	}
	if err := unix.Renameat2(int(destination.parent.Fd()), name, int(destination.parent.Fd()), destination.name, flags); err != nil {
		return err
	}
	return destination.parent.Sync()
}

func (destination *transferDestination) removeTemporary(name string) error {
	if !validTemporaryName(name) {
		return ErrStorage
	}
	err := unix.Unlinkat(int(destination.parent.Fd()), name, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return destination.parent.Sync()
}

func transferFileIdentity(file *os.File) (string, error) {
	var info unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &info); err != nil {
		return "", ErrStorage
	}
	return strconv.FormatUint(uint64(info.Dev), 16) + ":" + strconv.FormatUint(info.Ino, 16), nil
}

func transferFileVersion(file *os.File) (string, error) {
	var info unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &info); err != nil {
		return "", ErrStorage
	}
	return fmt.Sprintf("%x:%x:%d:%d:%d:%d:%d", info.Dev, info.Ino, info.Size, info.Mtim.Sec, info.Mtim.Nsec, info.Ctim.Sec, info.Ctim.Nsec), nil
}

func setTransferMode(file *os.File, mode uint32) error { return file.Chmod(os.FileMode(mode)) }

func nativeNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }

func openCheckedTransfer(parent *os.File, relative string, device uint64) (*os.File, error) {
	reference, err := openRelative(parent, relative, unix.O_PATH)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reference.Close() }()
	if err := validateTransferFile(reference, device); err != nil {
		return nil, err
	}
	descriptor, err := unix.Open("/proc/self/fd/"+strconv.Itoa(int(reference.Fd())), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), "operation-source")
	if err := validateTransferFile(file, device); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// 32 位 Linux 上 statfs.f_type 是 int32，BTRFS/RAMFS/F2FS 魔数超出 int32 范围；
// 统一按 32 位无符号位模式比较，既能在 386/arm 上编译，也不会把发行版魔数判成未知文件系统。
func transferSuperMagic(value int64) uint32 {
	return uint32(value)
}

func safeTransferFilesystem(descriptor int) bool {
	var info unix.Statfs_t
	if err := unix.Fstatfs(descriptor, &info); err != nil {
		return false
	}
	switch transferSuperMagic(int64(info.Type)) {
	case transferSuperMagic(unix.EXT4_SUPER_MAGIC), transferSuperMagic(unix.XFS_SUPER_MAGIC), transferSuperMagic(unix.BTRFS_SUPER_MAGIC),
		transferSuperMagic(unix.TMPFS_MAGIC), transferSuperMagic(unix.OVERLAYFS_SUPER_MAGIC), transferSuperMagic(unix.RAMFS_MAGIC),
		transferSuperMagic(unix.F2FS_SUPER_MAGIC):
		return true
	default:
		return false
	}
}

func transferModeMatches(file *os.File, mode uint32) bool {
	info, err := file.Stat()
	return err == nil && info.Mode().Perm() == os.FileMode(mode)
}
