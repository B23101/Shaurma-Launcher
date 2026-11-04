//go:build windows

package access

import (
	"errors"
	"syscall"
	"unsafe"
)

// Ключ пристрою на Windows шифрується DPAPI (прив'язка до користувача ОС).
var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (b *dataBlob) bytes() []byte {
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

const cryptProtectUIForbidden = 0x1

var entropy = []byte("shaurma-device-key-v1")

func protect(plain []byte) ([]byte, bool, error) {
	var out dataBlob
	r, _, e := procCryptProtectData.Call(uintptr(unsafe.Pointer(newBlob(plain))), 0, uintptr(unsafe.Pointer(newBlob(entropy))), 0, 0, cryptProtectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, false, errors.New("DPAPI: " + e.Error())
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), false, nil
}

func unprotect(sealed []byte) ([]byte, error) {
	var out dataBlob
	r, _, e := procCryptUnprotectData.Call(uintptr(unsafe.Pointer(newBlob(sealed))), 0, uintptr(unsafe.Pointer(newBlob(entropy))), 0, 0, cryptProtectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errors.New("DPAPI: " + e.Error())
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}
