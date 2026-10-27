package mods

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Символ "%" в імені jar ламає запуск: Forge/NeoForge/Fabric будують із
// шляху файлу URI, і літеральний "%2B" чи "%20" в імені перетворюється в
// "+"/пробіл — модуль не знаходиться, і Minecraft просто не завантажує мод
// (або падає на старті). Такі імена з'являються, коли файл збережено з
// CDN-посилання без розкодування ("mod-1.0%2B1.20.1.jar").

var pctRun = regexp.MustCompile(`(?:%[0-9A-Fa-f]{2})+`)

// SafeFileName розкодовує послідовності %XX (якщо вони складають валідний
// UTF-8), а все, що лишилось від "%" (одинокий "100%.jar"), замінює на "_".
// Імена без "%" повертаються без змін.
func SafeFileName(name string) string {
	if !strings.Contains(name, "%") {
		return name
	}
	name = pctRun.ReplaceAllStringFunc(name, func(run string) string {
		b, err := hex.DecodeString(strings.ReplaceAll(run, "%", ""))
		if err != nil || !utf8.Valid(b) {
			return run
		}
		return string(b)
	})
	// Розкодований результат не має містити роздільників шляху.
	name = strings.NewReplacer("/", "_", "\\", "_", "%", "_").Replace(name)
	return name
}

// FixUnsafeFileNames перейменовує jar-файли (і .jar.disabled) з "%" в імені
// на безпечні. Повертає пари old→new для лога. Якщо цільове ім'я вже зайняте
// файлом того самого розміру — це дублікат, "%"-копію видаляємо; інакше
// додаємо числовий суфікс, щоб нічого не перезаписати.
func FixUnsafeFileNames(modsDir string) [][2]string {
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil
	}
	var done [][2]string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		old := e.Name()
		if !strings.Contains(old, "%") {
			continue
		}
		if !strings.HasSuffix(old, ".jar") && !strings.HasSuffix(old, disabledSuffix) {
			continue
		}
		fixed := SafeFileName(old)
		if fixed == old {
			continue
		}
		src := filepath.Join(modsDir, old)
		dst := filepath.Join(modsDir, fixed)
		if dstInfo, err := os.Stat(dst); err == nil {
			srcInfo, serr := os.Stat(src)
			if serr == nil && srcInfo.Size() == dstInfo.Size() {
				if os.Remove(src) == nil {
					done = append(done, [2]string{old, fixed})
				}
				continue
			}
			dst = uniqueName(modsDir, fixed)
		}
		if os.Rename(src, dst) == nil {
			done = append(done, [2]string{old, filepath.Base(dst)})
		}
	}
	return done
}

func uniqueName(dir, name string) string {
	ext := ""
	base := name
	if strings.HasSuffix(base, disabledSuffix) {
		ext = disabledSuffix
		base = strings.TrimSuffix(base, disabledSuffix)
	} else if strings.HasSuffix(base, ".jar") {
		ext = ".jar"
		base = strings.TrimSuffix(base, ".jar")
	}
	for i := 1; ; i++ {
		cand := filepath.Join(dir, base+"_"+itoa(i)+ext)
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}
