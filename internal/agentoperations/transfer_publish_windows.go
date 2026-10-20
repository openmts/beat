//go:build windows

package agentoperations

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type nativeRenameInformation struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

func renameNative(file *os.File, destination *transferDestination, overwrite bool) error {
	name, err := windows.UTF16FromString(destination.name)
	if err != nil {
		return ErrPolicy
	}
	bytesLength := (len(name) - 1) * 2
	var layout nativeRenameInformation
	size := int(unsafe.Offsetof(layout.FileName)) + bytesLength
	buffer := make([]byte, max(size, int(unsafe.Sizeof(layout))))
	information := (*nativeRenameInformation)(unsafe.Pointer(&buffer[0]))
	if overwrite {
		information.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS
	}
	information.RootDirectory = windows.Handle(destination.parent.Fd())
	information.FileNameLength = uint32(bytesLength)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&information.FileName[0])), len(name)-1), name[:len(name)-1])
	var status windows.IO_STATUS_BLOCK
	if err := windows.NtSetInformationFile(windows.Handle(file.Fd()), &status, &buffer[0], uint32(size), windows.FileRenameInformation); err != nil {
		return err
	}
	return file.Sync()
}

func setTransferMode(file *os.File, mode uint32) error {
	sid, err := currentSID()
	if err != nil {
		return err
	}
	rights := uint32(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC)
	if mode == 0o700 {
		rights |= windows.FILE_GENERIC_EXECUTE
	} else if mode != 0o600 {
		return ErrPolicy
	}
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;0x%x;;;%s)", rights, sid.String()))
	if err != nil {
		return ErrStorage
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return ErrStorage
	}
	return windows.SetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, sid, nil, acl, nil)
}

func validateHandleDACL(file *os.File, owned bool) error {
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return ErrPolicy
	}
	extra, err := operationsSID()
	if err != nil {
		return err
	}
	return validateDescriptorWithSID(descriptor, owned, extra)
}

func transferModeMatches(file *os.File, mode uint32) bool {
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	acl, _, err := descriptor.DACL()
	if err != nil || acl == nil {
		return false
	}
	sid, err := currentSID()
	if err != nil {
		return false
	}
	for index := uint16(0); index < acl.AceCount; index++ {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, uint32(index), &entry); err != nil {
			return false
		}
		trustee := (*windows.SID)(unsafe.Pointer(&entry.SidStart))
		if trustee.Equals(sid) {
			return (entry.Mask&windows.FILE_GENERIC_EXECUTE == windows.FILE_GENERIC_EXECUTE) == (mode == 0o700)
		}
	}
	return false
}
