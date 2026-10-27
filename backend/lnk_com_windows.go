//go:build windows

package main

// lnk_com_windows.go — створення .lnk через COM (IShellLinkW + IPersistFile)
// напряму з Go, без сторонніх процесів.
//
// Раніше createLNK запускав прихований `powershell.exe -WindowStyle Hidden`
// зі скриптом WScript.Shell. Прихований PowerShell із GUI-застосунку — типова
// поведінкова ознака дроппера/стилера для антивірусних ML-евристик, тому
// ярлики створюються нативно: тільки стандартна бібліотека (syscall +
// ole32.dll), нових залежностей немає.

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modOle32             = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize   = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
)

type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	// {00021401-0000-0000-C000-000000000046}
	clsidShellLink = comGUID{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// {000214F9-0000-0000-C000-000000000046}
	iidIShellLinkW = comGUID{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// {0000010B-0000-0000-C000-000000000046}
	iidIPersistFile = comGUID{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
)

// Індекси методів у vtable (IUnknown: 0..2).
const (
	vtRelease            = 2
	vtQueryInterface     = 0
	vtShellSetDesc       = 7  // IShellLinkW::SetDescription
	vtShellSetWorkDir    = 9  // IShellLinkW::SetWorkingDirectory
	vtShellSetArgs       = 11 // IShellLinkW::SetArguments
	vtShellSetIconLoc    = 17 // IShellLinkW::SetIconLocation
	vtShellSetPath       = 20 // IShellLinkW::SetPath
	vtPersistFileSave    = 6  // IPersistFile::Save
	ptrSize              = unsafe.Sizeof(uintptr(0))
	hresultFailureSignal = 0x80000000
)

// comCall викликає метод idx COM-об'єкта obj (перший аргумент — this).
func comCall(obj uintptr, idx uintptr, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj)) //nolint:govet // COM vtable
	fn := *(*uintptr)(unsafe.Pointer(vtbl + idx*ptrSize))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, obj)
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func hrFailed(hr uintptr) bool { return uint32(hr)&hresultFailureSignal != 0 }

func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, vtRelease)
	}
}

// splitIconLocation розбирає "path,index" (формат WScript IconLocation).
func splitIconLocation(loc string) (string, int) {
	if i := strings.LastIndex(loc, ","); i > 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(loc[i+1:])); err == nil {
			return loc[:i], n
		}
	}
	return loc, 0
}

// createShellLink створює .lnk (UTF-16 шляхи, WinAPI). iconLocation ""
// = іконка цільового exe за замовчуванням.
func createShellLink(lnkPath, exePath, args, workingDir, iconLocation, description string) error {
	// COM-апартамент прив'язаний до потоку.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	switch {
	case uint32(hr) == rpcEChangedMode:
		// Потік уже ініціалізований в іншому режимі — працюємо як є, без Uninitialize.
	case hrFailed(hr):
		return fmt.Errorf("CoInitializeEx: 0x%08X", uint32(hr))
	default:
		defer procCoUninitialize.Call()
	}

	var psl uintptr
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&psl)))
	if hrFailed(hr) || psl == 0 {
		return fmt.Errorf("CoCreateInstance(ShellLink): 0x%08X", uint32(hr))
	}
	defer comRelease(psl)

	// UTF-16 рядки мають жити до кінця викликів (KeepAlive нижче).
	var strs [5]*uint16
	for n, v := range []string{exePath, args, workingDir, description, lnkPath} {
		p, err := syscall.UTF16PtrFromString(v)
		if err != nil {
			return fmt.Errorf("createShellLink: %w", err)
		}
		strs[n] = p
	}
	iconPath, iconIdx := splitIconLocation(iconLocation)
	var iconPtr *uint16
	if iconPath != "" {
		p, err := syscall.UTF16PtrFromString(iconPath)
		if err != nil {
			return fmt.Errorf("createShellLink: %w", err)
		}
		iconPtr = p
	}
	defer runtime.KeepAlive(strs)
	defer runtime.KeepAlive(iconPtr)

	check := func(what string, hr uintptr) error {
		if hrFailed(hr) {
			return fmt.Errorf("%s: 0x%08X", what, uint32(hr))
		}
		return nil
	}
	if err := check("SetPath", comCall(psl, vtShellSetPath, uintptr(unsafe.Pointer(strs[0])))); err != nil {
		return err
	}
	if err := check("SetArguments", comCall(psl, vtShellSetArgs, uintptr(unsafe.Pointer(strs[1])))); err != nil {
		return err
	}
	if err := check("SetWorkingDirectory", comCall(psl, vtShellSetWorkDir, uintptr(unsafe.Pointer(strs[2])))); err != nil {
		return err
	}
	if err := check("SetDescription", comCall(psl, vtShellSetDesc, uintptr(unsafe.Pointer(strs[3])))); err != nil {
		return err
	}
	if iconPtr != nil {
		if err := check("SetIconLocation", comCall(psl, vtShellSetIconLoc, uintptr(unsafe.Pointer(iconPtr)), uintptr(iconIdx))); err != nil {
			return err
		}
	}

	var ppf uintptr
	if err := check("QueryInterface(IPersistFile)",
		comCall(psl, vtQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&ppf)))); err != nil {
		return err
	}
	defer comRelease(ppf)
	// fRemember = TRUE: файл стає «поточним» для об'єкта (як WScript Save).
	return check("IPersistFile.Save", comCall(ppf, vtPersistFileSave, uintptr(unsafe.Pointer(strs[4])), 1))
}
