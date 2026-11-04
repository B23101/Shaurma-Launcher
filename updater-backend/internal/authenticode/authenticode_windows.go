//go:build windows

// Package authenticode перевіряє підпис Authenticode завантажених exe/dll
// (§5.4, §5.6): чинність і ланцюжок (WinVerifyTrust), а потім видавця за
// іменем суб'єкта і, якщо задано, за відбитком із keyset. Відбитки береться
// з набору ключів, підписаного root, тож перевипуск сертифіката SignPath не
// ламає оновлення: новий відбиток додається в keyset заздалегідь.
package authenticode

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"shaurma-trust"
)

var (
	wintrust                       = syscall.NewLazyDLL("wintrust.dll")
	crypt32                        = syscall.NewLazyDLL("crypt32.dll")
	procWinVerifyTrust             = wintrust.NewProc("WinVerifyTrust")
	procCryptQueryObject           = crypt32.NewProc("CryptQueryObject")
	procCryptMsgGetParam           = crypt32.NewProc("CryptMsgGetParam")
	procCryptMsgClose              = crypt32.NewProc("CryptMsgClose")
	procCertCloseStore             = crypt32.NewProc("CertCloseStore")
	procCertFindCertificateInStore = crypt32.NewProc("CertFindCertificateInStore")
	procCertFreeCertificateContext = crypt32.NewProc("CertFreeCertificateContext")
	procCertGetNameStringW         = crypt32.NewProc("CertGetNameStringW")
	procCertGetCertCtxProperty     = crypt32.NewProc("CertGetCertificateContextProperty")
)

// WINTRUST_ACTION_GENERIC_VERIFY_V2 {00AAC56B-CD44-11d0-8CC2-00C04FC295EE}
var actionGenericVerifyV2 = syscall.GUID{Data1: 0x00AAC56B, Data2: 0xCD44, Data3: 0x11d0, Data4: [8]byte{0x8C, 0xC2, 0x00, 0xC0, 0x4F, 0xC2, 0x95, 0xEE}}

type wintrustFileInfo struct {
	cbStruct       uint32
	pcwszFilePath  *uint16
	hFile          uintptr
	pgKnownSubject uintptr
}

type wintrustData struct {
	cbStruct            uint32
	pPolicyCallbackData uintptr
	pSIPClientData      uintptr
	dwUIChoice          uint32
	fdwRevocationChecks uint32
	dwUnionChoice       uint32
	pFile               *wintrustFileInfo
	dwStateAction       uint32
	hWVTStateData       uintptr
	pwszURLReference    *uint16
	dwProvFlags         uint32
	dwUIContext         uint32
	pSignatureSettings  uintptr
}

const (
	wtdUINone              = 2
	wtdRevokeNone          = 0
	wtdChoiceFile          = 1
	wtdStateActionVerify   = 1
	wtdStateActionClose    = 2
	wtdCacheOnlyURLRetriev = 0x1000 // мітку часу перевіряємо без мережевих звернень

	certQueryObjectFile       = 1
	certQueryContentFlagPKCS7 = 1 << 10 // CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED
	certQueryFormatFlagBinary = 1 << 1
	cmsgSignerInfoParam       = 6
	encoding                  = 0x00000001 | 0x00010000 // X509_ASN_ENCODING | PKCS_7_ASN_ENCODING
	certFindSubjectCert       = 0x000B0000
	certNameSimpleDisplayType = 4
	certSHA1HashPropID        = 3
)

type blob struct {
	cbData uint32
	pbData *byte
}

type algID struct {
	pszObjID   *byte
	parameters blob
}

type bitBlob struct {
	cbData      uint32
	pbData      *byte
	cUnusedBits uint32
}

type certInfo struct {
	dwVersion           uint32
	serialNumber        blob
	signatureAlgorithm  algID
	issuer              blob
	notBefore           [2]uint32
	notAfter            [2]uint32
	subject             blob
	subjectPublicKeyAlg algID
	subjectPublicKey    bitBlob
	issuerUniqueID      bitBlob
	subjectUniqueID     bitBlob
	cExtension          uint32
	rgExtension         uintptr
}

// cmsgSignerInfo — лише початок CMSG_SIGNER_INFO (нам потрібні Issuer і Serial).
type cmsgSignerInfo struct {
	dwVersion    uint32
	issuer       blob
	serialNumber blob
}

// ErrUnsigned — файл без чинного підпису.
var ErrUnsigned = errors.New("файл не має чинного підпису Authenticode")

// Verify перевіряє підпис файлу. signers порожній — достатньо чинного
// підпису з довіреним ланцюжком (до появи видавців у keyset).
func Verify(path string, signers []trust.AuthenticodeSigner) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	fi := wintrustFileInfo{pcwszFilePath: p}
	fi.cbStruct = uint32(unsafe.Sizeof(fi))
	wd := wintrustData{
		dwUIChoice:          wtdUINone,
		fdwRevocationChecks: wtdRevokeNone,
		dwUnionChoice:       wtdChoiceFile,
		pFile:               &fi,
		dwStateAction:       wtdStateActionVerify,
		dwProvFlags:         wtdCacheOnlyURLRetriev,
	}
	wd.cbStruct = uint32(unsafe.Sizeof(wd))
	r, _, _ := procWinVerifyTrust.Call(^uintptr(0) /* INVALID_HANDLE_VALUE */, uintptr(unsafe.Pointer(&actionGenericVerifyV2)), uintptr(unsafe.Pointer(&wd)))
	wd.dwStateAction = wtdStateActionClose
	procWinVerifyTrust.Call(^uintptr(0), uintptr(unsafe.Pointer(&actionGenericVerifyV2)), uintptr(unsafe.Pointer(&wd)))
	if r != 0 {
		return fmt.Errorf("%w (код 0x%08X)", ErrUnsigned, uint32(r))
	}
	if len(signers) == 0 {
		return nil
	}
	subject, thumb, err := Signer(path)
	if err != nil {
		return err
	}
	for _, s := range signers {
		if !strings.EqualFold(strings.TrimSpace(s.Subject), subject) {
			continue
		}
		if len(s.Thumbprints) == 0 {
			return nil
		}
		for _, t := range s.Thumbprints {
			if strings.EqualFold(strings.ReplaceAll(t, " ", ""), thumb) {
				return nil
			}
		}
	}
	return fmt.Errorf("невідомий видавець підпису %q (%s)", subject, thumb)
}

// Signer повертає ім'я суб'єкта і SHA-1 відбиток листового сертифіката.
func Signer(path string) (subject, thumbprint string, err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", "", err
	}
	var hStore, hMsg uintptr
	r, _, e := procCryptQueryObject.Call(certQueryObjectFile, uintptr(unsafe.Pointer(p)),
		certQueryContentFlagPKCS7, certQueryFormatFlagBinary, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&hStore)), uintptr(unsafe.Pointer(&hMsg)), 0)
	if r == 0 {
		return "", "", fmt.Errorf("CryptQueryObject: %v", e)
	}
	defer procCertCloseStore.Call(hStore, 0)
	defer procCryptMsgClose.Call(hMsg)

	var n uint32
	if r, _, e := procCryptMsgGetParam.Call(hMsg, cmsgSignerInfoParam, 0, 0, uintptr(unsafe.Pointer(&n))); r == 0 || n == 0 {
		return "", "", fmt.Errorf("CryptMsgGetParam: %v", e)
	}
	buf := make([]byte, n)
	if r, _, e := procCryptMsgGetParam.Call(hMsg, cmsgSignerInfoParam, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return "", "", fmt.Errorf("CryptMsgGetParam: %v", e)
	}
	si := (*cmsgSignerInfo)(unsafe.Pointer(&buf[0]))
	ci := certInfo{issuer: si.issuer, serialNumber: si.serialNumber}
	ctx, _, e := procCertFindCertificateInStore.Call(hStore, encoding, 0, certFindSubjectCert, uintptr(unsafe.Pointer(&ci)), 0)
	if ctx == 0 {
		return "", "", fmt.Errorf("CertFindCertificateInStore: %v", e)
	}
	defer procCertFreeCertificateContext.Call(ctx)

	name := make([]uint16, 512)
	nn, _, _ := procCertGetNameStringW.Call(ctx, certNameSimpleDisplayType, 0, 0, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if nn > 1 {
		subject = syscall.UTF16ToString(name[:nn-1])
	}
	hash := make([]byte, 20)
	hl := uint32(len(hash))
	if r, _, e := procCertGetCertCtxProperty.Call(ctx, certSHA1HashPropID, uintptr(unsafe.Pointer(&hash[0])), uintptr(unsafe.Pointer(&hl))); r == 0 {
		return "", "", fmt.Errorf("CertGetCertificateContextProperty: %v", e)
	}
	return subject, fmt.Sprintf("%X", hash[:hl]), nil
}
