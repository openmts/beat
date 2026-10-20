//go:build windows

package agentoperations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func splitPath(path string) []string { return strings.Split(path, string(filepath.Separator)) }

func privateDirectory(path string, owned bool) error { return privatePath(path, true, owned) }

func privateFile(path string) error { return privatePath(path, false, true) }

func privatePath(path string, directory, owned bool) error {
	if err := checkPrivatePathType(path, directory); err != nil {
		return err
	}
	if err := validateDACL(path, owned); err != nil {
		return err
	}
	return checkSensitiveFile(path)
}

func checkPrivatePathType(path string, directory bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ErrStorage
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	if !strings.EqualFold(absolute, resolved) {
		return ErrStorage
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) {
		return ErrStorage
	}
	return nil
}

func currentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, ErrStorage
	}
	return user.User.Sid, nil
}

func validateDACL(path string, owned bool) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return ErrStorage
	}
	return validateDescriptorDACL(descriptor, owned)
}

func validateDescriptorDACL(descriptor *windows.SECURITY_DESCRIPTOR, owned bool) error {
	return validateDescriptorWithSID(descriptor, owned, nil)
}

func validateDescriptorWithSID(descriptor *windows.SECURITY_DESCRIPTOR, owned bool, extra *windows.SID) error {
	sid, err := currentSID()
	if err != nil {
		return err
	}
	acl, err := ownedDescriptorACL(descriptor, sid, owned)
	if err != nil {
		return err
	}
	for index := uint16(0); index < acl.AceCount; index++ {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, uint32(index), &entry); err != nil {
			return ErrStorage
		}
		if !allowedDescriptorEntry(entry, sid, extra) {
			return ErrStorage
		}
	}
	return nil
}

func secureCreatedDirectory(path string) error {
	sid, err := currentSID()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + sid.String() + ")")
	if err != nil {
		return ErrStorage
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return ErrStorage
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, sid, nil, acl, nil); err != nil {
		return ErrStorage
	}
	if err := setIntegrityLabel(path, "ME"); err != nil {
		return err
	}
	return privateDirectory(path, true)
}

func lockState(path string) (func() error, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, ErrStorage
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	handle, err := windows.CreateFile(name, access|windows.WRITE_DAC|windows.WRITE_OWNER, 0, nil, windows.CREATE_NEW, flags, 0)
	created := err == nil
	if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		handle, err = windows.CreateFile(name, access, 0, nil, windows.OPEN_EXISTING, flags, 0)
	}
	if err != nil {
		return nil, ErrStorage
	}
	if created {
		file := os.NewFile(uintptr(handle), path)
		if err := secureStateHandle(handle); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := privateFile(path); err != nil {
			_ = file.Close()
			return nil, err
		}
		return file.Close, nil
	}
	if err := privateFile(path); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return func() error { return windows.CloseHandle(handle) }, nil
}

func replacePrivate(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return ErrStorage
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return ErrStorage
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Windows 的持久化发布由 MoveFileEx WRITE_THROUGH 和文件 FlushFileBuffers 完成。
func syncDirectory(string) error { return nil }

func platformDeniedRoots() []string {
	return []string{`C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`}
}

func ownedDescriptorACL(descriptor *windows.SECURITY_DESCRIPTOR, sid *windows.SID, owned bool) (*windows.ACL, error) {
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || (owned && !owner.Equals(sid)) {
		return nil, ErrStorage
	}
	acl, _, err := descriptor.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return nil, ErrStorage
	}
	return acl, nil
}

func allowedDescriptorEntry(entry *windows.ACCESS_ALLOWED_ACE, current, extra *windows.SID) bool {
	if entry.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return false
	}
	trustee := (*windows.SID)(unsafe.Pointer(&entry.SidStart))
	return trustee.Equals(current) || trustee.String() == "S-1-5-18" || (extra != nil && trustee.Equals(extra))
}

func secureCreatedFile(file *os.File) error {
	sid, err := currentSID()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid.String() + ")")
	if err != nil {
		return ErrStorage
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return ErrStorage
	}
	return windows.SetNamedSecurityInfo(file.Name(), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, sid, nil, acl, nil)
}

func secureStateHandle(handle windows.Handle) error {
	sid, err := currentSID()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid.String() + ")")
	if err != nil {
		return ErrStorage
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return ErrStorage
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, sid, nil, acl, nil)
}
