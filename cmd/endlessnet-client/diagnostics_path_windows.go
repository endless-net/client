//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type diagnosticsFileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

func isDiagnosticsReparsePoint(info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return info != nil
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func diagnosticsDirectoryPermissionsSecure(path string, info os.FileInfo) bool {
	return info != nil && info.IsDir() && diagnosticsPathACLSecure(path, true)
}

func diagnosticsFilePermissionsSecure(path string, info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && diagnosticsPathACLSecure(path, false)
}

func diagnosticsPathACLSecure(path string, directory bool) bool {
	path = strings.TrimSpace(path)
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle) //nolint:errcheck // validation is complete before the handle is closed

	fileType, err := windows.GetFileType(handle)
	if err != nil || fileType != windows.FILE_TYPE_DISK {
		return false
	}
	var attributes diagnosticsFileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&attributes)),
		uint32(unsafe.Sizeof(attributes)),
	); err != nil || attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	if directory != (attributes.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) {
		return false
	}

	securityDescriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return false
	}
	control, _, err := securityDescriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	allowedSIDs, err := trustedDiagnosticsSIDs()
	if err != nil {
		return false
	}
	owner, _, err := securityDescriptor.Owner()
	if err != nil || !diagnosticsSIDAllowed(owner, allowedSIDs) {
		return false
	}
	dacl, _, err := securityDescriptor.DACL()
	if err != nil || dacl == nil {
		return false
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return false
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !diagnosticsSIDAllowed(sid, allowedSIDs) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func secureNewDiagnosticsDirectory(path string) error {
	return setProtectedDiagnosticsDACL(path, true)
}

func secureDiagnosticsBundleFile(path string) error {
	return setProtectedDiagnosticsDACL(path, false)
}

func setProtectedDiagnosticsDACL(path string, directory bool) error {
	allowedSIDs, err := trustedDiagnosticsSIDs()
	if err != nil {
		return err
	}
	aceFlags := ""
	if directory {
		aceFlags = "OICI"
	}
	var sddl strings.Builder
	sddl.WriteString("D:P")
	seen := make(map[string]struct{}, len(allowedSIDs))
	for _, sid := range allowedSIDs {
		value := sid.String()
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		sddl.WriteString("(A;" + aceFlags + ";FA;;;" + value + ")")
	}
	securityDescriptor, err := windows.SecurityDescriptorFromString(sddl.String())
	if err != nil {
		return err
	}
	dacl, _, err := securityDescriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

func trustedDiagnosticsSIDs() ([]*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	administratorsSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	return []*windows.SID{user.User.Sid, systemSID, administratorsSID}, nil
}

func diagnosticsSIDAllowed(sid *windows.SID, allowed []*windows.SID) bool {
	if sid == nil || !sid.IsValid() {
		return false
	}
	for _, candidate := range allowed {
		if sid.Equals(candidate) {
			return true
		}
	}
	return false
}
