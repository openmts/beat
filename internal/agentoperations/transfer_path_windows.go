//go:build windows

package agentoperations

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type transferRoot struct {
	file   *os.File
	volume uint32
}

type transferDestination struct {
	parent *os.File
	name   string
	volume uint32
}

type relativeOptions struct{ access, disposition, options uint32 }

func openTransferRoot(directory string) (*transferRoot, error) {
	if !filepath.IsAbs(directory) || strings.HasPrefix(directory, `\\`) {
		return nil, ErrPolicy
	}
	if err := validateOperationDACL(directory, true); err != nil {
		return nil, err
	}
	file, err := openNative(0, `\??\`+filepath.Clean(directory), relativeOptions{access: windows.FILE_GENERIC_READ, disposition: windows.FILE_OPEN, options: windows.FILE_DIRECTORY_FILE})
	if err != nil {
		return nil, ErrBoundary
	}
	info, err := transferInformation(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	volume := filepath.VolumeName(directory) + string(filepath.Separator)
	encoded, err := windows.UTF16PtrFromString(volume)
	if err != nil || windows.GetDriveType(encoded) != windows.DRIVE_FIXED {
		_ = file.Close()
		return nil, ErrBoundary
	}
	return &transferRoot{file: file, volume: info.VolumeSerialNumber}, nil
}

func (root *transferRoot) Close() error { return root.file.Close() }

func openNative(parent windows.Handle, name string, options relativeOptions) (*os.File, error) {
	encoded, err := windows.NewNTUnicodeString(strings.ReplaceAll(name, "/", `\`))
	if err != nil {
		return nil, ErrPolicy
	}
	attributes := windows.OBJECT_ATTRIBUTES{RootDirectory: parent, ObjectName: encoded, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, options.access|windows.SYNCHRONIZE, &attributes, &status, nil, windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, options.disposition, options.options|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), "operation-file"), nil
}

func (root *transferRoot) openSource(relative string) (*os.File, error) {
	file, err := openNative(windows.Handle(root.file.Fd()), relative, relativeOptions{access: windows.FILE_GENERIC_READ, disposition: windows.FILE_OPEN, options: windows.FILE_NON_DIRECTORY_FILE})
	if err != nil {
		return nil, err
	}
	if err := validateTransferFile(file, root.volume); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (root *transferRoot) destination(relative string) (*transferDestination, error) {
	parent, err := openNative(windows.Handle(root.file.Fd()), path.Dir(relative), relativeOptions{access: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE, disposition: windows.FILE_OPEN, options: windows.FILE_DIRECTORY_FILE})
	if err != nil {
		return nil, err
	}
	info, err := transferInformation(parent)
	if err != nil || info.VolumeSerialNumber != root.volume {
		_ = parent.Close()
		return nil, ErrPolicy
	}
	if err := validateHandleDACL(parent, true); err != nil {
		_ = parent.Close()
		return nil, err
	}
	return &transferDestination{parent: parent, name: path.Base(relative), volume: root.volume}, nil
}

func (destination *transferDestination) Close() error { return destination.parent.Close() }

func transferInformation(file *os.File) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return info, ErrStorage
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return info, ErrPolicy
	}
	return info, nil
}

func validateTransferFile(file *os.File, volume uint32) error {
	info, err := transferInformation(file)
	if err != nil {
		return err
	}
	kind, err := windows.GetFileType(windows.Handle(file.Fd()))
	if err != nil || kind != windows.FILE_TYPE_DISK || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || info.VolumeSerialNumber != volume || info.NumberOfLinks != 1 {
		return ErrPolicy
	}
	return validateHandleDACL(file, true)
}

func (destination *transferDestination) temporary(name string, create bool) (*os.File, error) {
	if !validTemporaryName(name) {
		return nil, ErrStorage
	}
	disposition := uint32(windows.FILE_OPEN)
	if create {
		disposition = windows.FILE_CREATE
	}
	file, err := openNative(windows.Handle(destination.parent.Fd()), name, relativeOptions{access: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER, disposition: disposition, options: windows.FILE_NON_DIRECTORY_FILE})
	if err != nil {
		return nil, err
	}
	if create {
		if err := setTransferMode(file, 0o600); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	if err := validateTransferFile(file, destination.volume); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (destination *transferDestination) current() (*os.File, error) {
	file, err := openNative(windows.Handle(destination.parent.Fd()), destination.name, relativeOptions{access: windows.FILE_GENERIC_READ, disposition: windows.FILE_OPEN, options: windows.FILE_NON_DIRECTORY_FILE})
	if err != nil {
		return nil, err
	}
	if err := validateTransferFile(file, destination.volume); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (destination *transferDestination) publish(name string, overwrite bool) (resultErr error) {
	existing, err := destination.current()
	if err == nil {
		_ = existing.Close()
		if !overwrite {
			return ErrFileConflict
		}
	} else if !nativeNotExist(err) {
		return err
	}
	file, err := destination.temporary(name, false)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	return renameNative(file, destination, overwrite)
}

func (destination *transferDestination) removeTemporary(name string) error {
	file, err := destination.temporary(name, false)
	if nativeNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	remove := byte(1)
	err = windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfo, &remove, 1)
	return errors.Join(err, file.Close())
}

func nativeNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(err, windows.STATUS_OBJECT_PATH_NOT_FOUND)
}

func transferFileIdentity(file *os.File) (string, error) {
	info, err := transferInformation(file)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%x:%x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

func transferFileVersion(file *os.File) (string, error) {
	info, err := transferInformation(file)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%x:%x:%d:%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow, info.FileSizeHigh, info.FileSizeLow, info.LastWriteTime.HighDateTime, info.LastWriteTime.LowDateTime), nil
}
