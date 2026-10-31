//go:build windows

package main

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ShellExecuteEx retains a process handle so success means the helper finished,
// not merely that the UAC dialog opened. No shell, scripts or DLL loading.
func elevateP2PFirewall() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	args, _ := windows.UTF16PtrFromString("--p2p-firewall-repair")
	info := struct {
		Size       uint32
		Mask       uint32
		Window     windows.Handle
		Verb       *uint16
		File       *uint16
		Parameters *uint16
		Directory  *uint16
		Show       int32
		Instance   windows.Handle
		IDList     uintptr
		Class      *uint16
		ClassKey   windows.Handle
		HotKey     uint32
		Icon       windows.Handle
		Process    windows.Handle
	}{Mask: 0x40 | 0x100, Verb: verb, File: file, Parameters: args, Show: windows.SW_SHOWNORMAL}
	info.Size = uint32(unsafe.Sizeof(info))
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return errors.New("p2p_repair_declined")
		}
		return errors.New("p2p_repair_failed")
	}
	if info.Process == 0 {
		return errors.New("p2p_repair_failed")
	}
	defer windows.CloseHandle(info.Process)
	result, err := windows.WaitForSingleObject(info.Process, 120000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		return errors.New("p2p_repair_pending")
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.Process, &code); err != nil || code != 0 {
		return errors.New("p2p_repair_failed")
	}
	return nil
}
