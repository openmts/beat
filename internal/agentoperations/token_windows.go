//go:build windows

package agentoperations

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const restrictedTokenFlags = 0x1

const fileDeleteChildRight = 0x40

func operationsSID() (*windows.SID, error) {
	current, err := currentSID()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte("beat.operations.write.v1:" + current.String()))
	value := fmt.Sprintf("S-1-5-21-%d-%d-%d-%d", binary.LittleEndian.Uint32(digest[0:4]), binary.LittleEndian.Uint32(digest[4:8]), binary.LittleEndian.Uint32(digest[8:12]), binary.LittleEndian.Uint32(digest[12:16]))
	sid, err := windows.StringToSid(value)
	if err != nil {
		return nil, ErrBoundary
	}
	return sid, nil
}

func restrictedCommandToken() (windows.Token, error) {
	if err := requireControllerIntegrity(); err != nil {
		return 0, err
	}
	restricting, err := restrictedCommandSIDs()
	if err != nil {
		return 0, err
	}
	procedure := windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")
	if err := procedure.Find(); err != nil {
		return 0, ErrBoundary
	}
	var source windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY, &source); err != nil {
		return 0, ErrBoundary
	}
	defer func() { _ = source.Close() }()
	var token windows.Token
	success, _, _ := procedure.Call(uintptr(source), restrictedTokenFlags, 0, 0, 0, 0, uintptr(len(restricting)), uintptr(unsafe.Pointer(&restricting[0])), uintptr(unsafe.Pointer(&token)))
	runtime.KeepAlive(restricting)
	if success == 0 {
		return 0, ErrBoundary
	}
	if err := lowerCommandIntegrity(token); err != nil {
		_ = token.Close()
		return 0, err
	}
	return token, nil
}

func restrictedCommandSIDs() ([]windows.SIDAndAttributes, error) {
	private, err := operationsSID()
	if err != nil {
		return nil, err
	}
	result := []windows.SIDAndAttributes{{Sid: private}}
	for _, kind := range []windows.WELL_KNOWN_SID_TYPE{windows.WinWorldSid, windows.WinAuthenticatedUserSid, windows.WinBuiltinUsersSid} {
		sid, err := windows.CreateWellKnownSid(kind)
		if err != nil {
			return nil, ErrBoundary
		}
		result = append(result, windows.SIDAndAttributes{Sid: sid})
	}
	return result, nil
}

func secureCommandScript(path string) error {
	current, err := currentSID()
	if err != nil {
		return err
	}
	restricting, err := operationsSID()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;FA;;;%s)(A;;0x%x;;;%s)", current.String(), windows.FILE_GENERIC_READ, restricting.String()))
	if err != nil {
		return ErrStorage
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return ErrStorage
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, current, nil, acl, nil)
}

func ensureCommandRootAccess(path string) error {
	current, err := currentSID()
	if err != nil {
		return err
	}
	restricting, err := operationsSID()
	if err != nil {
		return err
	}
	rights := uint32(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE | windows.DELETE | fileDeleteChildRight)
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;OICI;FA;;;%s)(A;OICI;FA;;;SY)(A;OICI;0x%x;;;%s)", current.String(), rights, restricting.String()))
	if err != nil {
		return ErrBoundary
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return ErrBoundary
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, current, nil, acl, nil); err != nil {
		return ErrBoundary
	}
	return setIntegrityLabel(path, "LW")
}

func validateOperationDirectory(path string) error {
	if err := checkPrivatePathType(path, true); err != nil {
		return err
	}
	return validateOperationDACL(path, false)
}

func validateOperationDACL(path string, owned bool) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return ErrPolicy
	}
	extra, err := operationsSID()
	if err != nil {
		return err
	}
	return validateDescriptorWithSID(descriptor, owned, extra)
}
