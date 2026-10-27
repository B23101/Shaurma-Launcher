package shaurmaid

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// hwidParts — стабільні апаратні/системні ознаки (реалізація залежить від ОС).
// MAC-адреси свідомо НЕ використовуються.
var hwidParts = collectHWIDParts

type hwidGen struct {
	once sync.Once
	val  string
	err  error
}

// NewHWIDGenerator — HWID = sha256("shaurma-hwid-v1|" + відсортовані ознаки). Рахується раз.
func NewHWIDGenerator() HWIDGenerator { return &hwidGen{} }

func (g *hwidGen) HWID() (string, error) {
	g.once.Do(func() { g.val, g.err = computeHWID(hwidParts()) })
	return g.val, g.err
}

func computeHWID(parts map[string]string) (string, error) {
	var keys []string
	n := 0
	for k, v := range parts {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || v == "unknown" || strings.Contains(v, "to be filled") || v == "default string" {
			continue
		}
		parts[k] = v
		keys = append(keys, k)
		n++
	}
	// Без хоча б одного «сильного» джерела (machine-id/MachineGuid) відбиток ненадійний.
	if parts["machine"] == "" || n < 1 {
		return "", errors.New("shaurmaid: не вдалося отримати апаратні ознаки ПК")
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("shaurma-hwid-v1|" + runtime.GOOS)
	for _, k := range keys {
		b.WriteString("|" + k + "=" + parts[k])
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:]), nil
}
