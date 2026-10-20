//go:build windows

package agentoperations

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const mediumIntegrityRID = 8192

const mandatoryLabelACEType = 0x11

func lowerCommandIntegrity(token windows.Token) error {
	sid, err := windows.StringToSid("S-1-16-4096")
	if err != nil {
		return ErrBoundary
	}
	label := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: sid, Attributes: windows.SE_GROUP_INTEGRITY}}
	err = windows.SetTokenInformation(token, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label)), label.Size())
	runtime.KeepAlive(sid)
	if err != nil {
		return ErrBoundary
	}
	return nil
}

func requireControllerIntegrity() error {
	var size uint32
	err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenIntegrityLevel, nil, 0, &size)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || size == 0 || size > 4096 {
		return ErrBoundary
	}
	content := make([]byte, size)
	if err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenIntegrityLevel, &content[0], size, &size); err != nil {
		return ErrBoundary
	}
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&content[0]))
	count := label.Label.Sid.SubAuthorityCount()
	if count == 0 || label.Label.Sid.SubAuthority(uint32(count-1)) < mediumIntegrityRID {
		return ErrBoundary
	}
	return nil
}

func setIntegrityLabel(path, label string) error {
	descriptor, err := windows.SecurityDescriptorFromString("S:(ML;OICI;NW;;;" + label + ")")
	if err != nil {
		return ErrStorage
	}
	acl, _, err := descriptor.SACL()
	if err != nil {
		return ErrStorage
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, acl)
}

func checkSensitiveFile(path string) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		return ErrStorage
	}
	acl, _, err := descriptor.SACL()
	if err != nil {
		return ErrStorage
	}
	if acl == nil {
		return nil
	}
	for index := uint16(0); index < acl.AceCount; index++ {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, uint32(index), &entry); err != nil {
			return ErrStorage
		}
		if entry.Header.AceType != mandatoryLabelACEType {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&entry.SidStart))
		count := sid.SubAuthorityCount()
		if count == 0 || sid.SubAuthority(uint32(count-1)) < mediumIntegrityRID {
			return ErrStorage
		}
	}
	return nil
}
