package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/creds"
)

// Доведення заміни заблокованих компонентів (updater.dll/.so і exe лаунчера)
// для лаунчерів, чий shell не може цього зробити сам.
//
// Shell до 2.7.28 вшитий з публічним ключем, який НЕ відповідає ключу, яким
// Pack Manager підписує launcher-version.json. Такий shell відкидає кожен
// updater.dll.new і <exe>.new («підпис маніфесту недійсний»), тож користувач
// назавжди лишається зі старими updater і shell, хоча backend уже новий (його
// оновлювач ставить напряму). Отже, нова система оновлень і сертифікат повної
// збірки до нього не доходять.
//
// Backend перевіряє .new ТИМ САМИМ способом, що й shell (підписаний маніфест
// із CDN, той самий контекст підпису, SHA-256 і розмір), але правильним
// ключем, і ставить файли на місце. Windows дозволяє перейменувати і
// завантажену DLL, і запущений exe; нові файли підхопляться з наступного
// запуску. Без .new або без валідного підпису нічого не змінюється.

// legacyManifestKeys — публічні ключі підпису launcher-version.json (base64,
// через кому). Можна перевизначити через -ldflags.
var legacyManifestKeys = "+G1ryfnOFIzk5EHTytppqR2jFPGiFi1KIjTeuIZrv9k="

const legacyManifestContext = "shaurma-launcher-manifest-v1\n"

// legacyFinishBase — origin старого CDN (у тестах — httptest).
var legacyFinishBase = cdnBaseURL

const legacyManifestMax = 2 << 20

// FinishResult — що зроблено (для журналу).
type FinishResult struct {
	Applied  []string
	Rejected map[string]string
}

type legacyComponent struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Kind   string `json:"kind"`
}

// FinishLegacyPending довстановлює <installDir>/updater.*.new і <exe>.new.
// exePath — шлях до exe лаунчера (os.Executable процесу shell).
func FinishLegacyPending(installDir, exePath string) (*FinishResult, error) {
	res := &FinishResult{Rejected: map[string]string{}}
	if token == "" {
		return res, nil // єдиний лаунчер: оновлює нова система, не цей шлях
	}
	names := []string{updaterLibName()}
	if exePath != "" && sameDir(filepath.Dir(exePath), installDir) {
		names = append(names, filepath.Base(exePath))
	}
	var pending []string
	for _, n := range names {
		if fi, err := os.Stat(filepath.Join(installDir, n+".new")); err == nil && fi.Mode().IsRegular() {
			pending = append(pending, n)
		}
	}
	if len(pending) == 0 {
		return res, nil
	}

	comps, err := fetchVerifiedLegacyComponents()
	if err != nil {
		return res, err
	}
	for _, name := range pending {
		if err := verifyPendingAgainst(installDir, name, comps); err != nil {
			res.Rejected[name] = err.Error()
			continue
		}
		if err := swapPending(installDir, name, name == filepath.Base(exePath)); err != nil {
			res.Rejected[name] = err.Error()
			continue
		}
		res.Applied = append(res.Applied, name)
	}
	return res, nil
}

func updaterLibName() string {
	if runtime.GOOS == "windows" {
		return "updater.dll"
	}
	return "updater.so"
}

func platformSeg() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return runtime.GOOS + "/"
}

func sameDir(a, b string) bool {
	ca, _ := filepath.Abs(a)
	cb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

// fetchVerifiedLegacyComponents — компоненти з УСІХ підписаних маніфестів
// (stable і beta): .new міг прийти з будь-якого каналу.
func fetchVerifiedLegacyComponents() ([]legacyComponent, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var all []legacyComponent
	var last error
	for _, file := range []string{"launcher-version.json", "launcher-version-beta.json"} {
		u := legacyFinishBase + "/launcher/" + platformSeg() + file
		payload, err := legacyGet(client, u)
		if err != nil {
			last = err
			continue
		}
		sig, err := legacyGet(client, u+".sig")
		if err != nil {
			last = err
			continue
		}
		if err := verifyLegacySig(payload, string(sig)); err != nil {
			last = fmt.Errorf("%s: %w", file, err)
			continue
		}
		var m struct {
			Components []legacyComponent `json:"components"`
		}
		if err := json.Unmarshal(bytes.TrimPrefix(payload, []byte{0xef, 0xbb, 0xbf}), &m); err != nil {
			last = err
			continue
		}
		all = append(all, m.Components...)
	}
	if len(all) == 0 {
		if last == nil {
			last = errors.New("немає підписаного маніфесту")
		}
		return nil, last
	}
	return all, nil
}

func legacyGet(client *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	creds.Apply(req.Header, token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d від %s", resp.StatusCode, u)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, legacyManifestMax+1))
	if err != nil {
		return nil, err
	}
	if len(b) > legacyManifestMax {
		return nil, errors.New("маніфест завеликий")
	}
	return b, nil
}

func verifyLegacySig(payload []byte, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("підпис маніфесту недійсний")
	}
	msg := append([]byte(legacyManifestContext), payload...)
	for _, part := range strings.Split(legacyManifestKeys, ",") {
		k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part))
		if err != nil || len(k) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(k), msg, sig) {
			return nil
		}
	}
	return errors.New("підпис маніфесту недійсний")
}

func verifyPendingAgainst(dir, name string, comps []legacyComponent) error {
	var lastErr error = fmt.Errorf("%s відсутній у підписаному маніфесті", name)
	for _, c := range comps {
		if c.Path != name || filepath.Base(c.Path) != c.Path || c.Kind == "archive" || c.Size <= 0 || len(c.SHA256) != 64 {
			continue
		}
		f, err := os.Open(filepath.Join(dir, name+".new"))
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, c.Size+1))
		f.Close()
		if err != nil {
			return err
		}
		if n == c.Size && strings.EqualFold(hex.EncodeToString(h.Sum(nil)), c.SHA256) {
			return nil
		}
		lastErr = fmt.Errorf("%s.new не збігається з підписаним маніфестом", name)
	}
	return lastErr
}

// swapPending: старий файл убік, новий на місце; при збої — відкат.
// exe відкладається як "<exe>~" (новий shell сам прибирає його при старті),
// бібліотека — як ".previous" (як і в shell/install).
func swapPending(dir, name string, isExe bool) error {
	dest := filepath.Join(dir, name)
	newPath := dest + ".new"
	backup := dest + ".previous"
	if isExe {
		backup = dest + "~"
	}
	_ = os.Remove(backup)
	hadOld := false
	if err := os.Rename(dest, backup); err == nil {
		hadOld = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(newPath, dest); err != nil {
		if hadOld {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	_ = os.Remove(newPath + ".proof")
	return nil
}
