package shaurmaid

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

// sealedPrefix відрізняє зашифрований рядок від відкритого тексту, щоб старі
// (незашифровані) значення читались як є й мігрували при першому збереженні.
const sealedPrefix = "enc1:"

// Sealer шифрує окремі рядкові секрети (токени) засобами ОС: Windows — DPAPI
// (прив'язка до користувача Windows), інші ОС — AES-GCM з ключем із HWID та солі
// (див. protector). Результат — рядок "enc1:<base64>", придатний для JSON.
type Sealer struct{ p protector }

// NewSealer створює Sealer; dir — тека для службових файлів протектора (сіль).
func NewSealer(dir string, hw HWIDGenerator) (*Sealer, error) {
	if hw == nil {
		return nil, errors.New("shaurmaid: nil HWID generator")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p, err := newProtector(dir, hw)
	if err != nil {
		return nil, err
	}
	return &Sealer{p: p}, nil
}

// IsSealed — чи значення вже зашифроване Sealer'ом.
func IsSealed(v string) bool { return strings.HasPrefix(v, sealedPrefix) }

// Seal шифрує plain. Порожній рядок лишається порожнім.
func (s *Sealer) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	b, err := s.p.protect([]byte(plain))
	if err != nil {
		return "", err
	}
	return sealedPrefix + base64.StdEncoding.EncodeToString(b), nil
}

// Open розшифровує значення, зашифроване Seal.
func (s *Sealer) Open(v string) (string, error) {
	if !IsSealed(v) {
		return "", errors.New("shaurmaid: value is not sealed")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, sealedPrefix))
	if err != nil {
		return "", err
	}
	plain, err := s.p.unprotect(raw)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
