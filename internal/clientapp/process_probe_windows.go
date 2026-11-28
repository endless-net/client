package clientapp

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func scanProcesses(visit func(name, path string)) bool {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var buf [32768]uint16
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		path := ""
		handle, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if e == nil {
			n := uint32(len(buf))
			if windows.QueryFullProcessImageName(handle, 0, &buf[0], &n) == nil {
				path = windows.UTF16ToString(buf[:n])
			}
			windows.CloseHandle(handle)
		}
		visit(name, path)
	}
	return errors.Is(err, windows.ERROR_NO_MORE_FILES)
}
