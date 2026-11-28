package clientapp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

var firmwareTable = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemFirmwareTable")

func platformPosture() (string, string, string) {
	v := windows.RtlGetVersion()
	version := fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	const rsmb = 0x52534d42
	size, _, _ := firmwareTable.Call(rsmb, 0, 0, 0)
	if size < 8 || size > 1<<20 {
		return version, version, ""
	}
	data := make([]byte, int(size))
	n, _, _ := firmwareTable.Call(rsmb, 0, uintptr(unsafe.Pointer(&data[0])), size)
	if n != size || int(binary.LittleEndian.Uint32(data[4:8])) != len(data)-8 {
		return version, version, ""
	}
	return version, version, smbiosSystemSerial(data[8:])
}

// SMBIOS system information (type 1) stores the one-based serial string index
// at offset 7. Each structure ends with a double NUL after its string table.
func smbiosSystemSerial(data []byte) string {
	for len(data) >= 4 {
		size := int(data[1])
		if size < 4 || size > len(data) {
			return ""
		}
		end := bytes.Index(data[size:], []byte{0, 0})
		if end < 0 {
			return ""
		}
		if data[0] == 1 && size >= 8 {
			idx := int(data[7])
			fields := bytes.Split(data[size:size+end], []byte{0})
			if idx > 0 && idx <= len(fields) {
				return systemSerial(string(fields[idx-1]))
			}
			return ""
		}
		if data[0] == 127 {
			return ""
		}
		data = data[size+end+2:]
	}
	return ""
}
