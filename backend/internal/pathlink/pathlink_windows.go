//go:build windows

package pathlink

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// MakeDirLink створює directory junction link → target (без прав адміністратора
// і без запуску cmd/PowerShell). link не має існувати.
func MakeDirLink(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.Mkdir(link, 0755); err != nil {
		return err
	}
	fail := func(e error) error { _ = os.Remove(link); return e }

	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return fail(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fail(err)
	}
	defer windows.CloseHandle(h)

	sub := utf16.Encode([]rune(`\??\` + abs))
	print := utf16.Encode([]rune(abs))
	subBytes, printBytes := len(sub)*2, len(print)*2
	pathBuf := make([]byte, 0, subBytes+printBytes+4)
	for _, u := range sub {
		pathBuf = binary.LittleEndian.AppendUint16(pathBuf, u)
	}
	pathBuf = append(pathBuf, 0, 0)
	for _, u := range print {
		pathBuf = binary.LittleEndian.AppendUint16(pathBuf, u)
	}
	pathBuf = append(pathBuf, 0, 0)

	buf := make([]byte, 0, 16+len(pathBuf))
	buf = binary.LittleEndian.AppendUint32(buf, 0xA0000003) // IO_REPARSE_TAG_MOUNT_POINT
	buf = binary.LittleEndian.AppendUint16(buf, uint16(8+len(pathBuf)))
	buf = binary.LittleEndian.AppendUint16(buf, 0)
	buf = binary.LittleEndian.AppendUint16(buf, 0)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(subBytes))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(subBytes+2))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(printBytes))
	buf = append(buf, pathBuf...)

	var ret uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &ret, nil); err != nil {
		return fail(fmt.Errorf("створення посилання на теку: %w", err))
	}
	return nil
}
