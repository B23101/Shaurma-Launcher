package minecraft

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Цілісність Forge/NeoForge та «база підказок» (hints) з VPS.
//
// Проблема, яку це закриває: binpatch (data/client.lzma) кешувався за ключем
// «версія MC», тому NeoForge 26.3 підсовував свій патч Forge 26.3 → патчений
// client.jar виходив чужий, а гра падала у FieldToMethodTransformer
// («Field ...LiquidBlock.fluid is not private and an instance field»).
// Далі checkForgeInstalled вважав такий jar «встановленим» (перевіряв лише zip).
//
// Тепер: (1) кеш ключується інсталером, (2) готовий patched jar звіряється
// з sha1 з install_profile.json (або з бази VPS) і при розбіжності
// перебудовується автоматично, (3) VPS може підказати правильні URL/хеші.

// LoaderHintsURL — адреса бази підказок на VPS/API (публікується з Pack Manager).
// Порожній рядок вимикає запит. Формат — loaderHintsFile.
var LoaderHintsURL = "https://api.shaurma-launcher.org/v1/loaders/hints.json"

// loaderHint — відомі-добрі дані для однієї версії лоадера.
type loaderHint struct {
	InstallerURLs []string `json:"installer_urls,omitempty"` // додаткові дзеркала installer.jar
	InstallerSHA1 string   `json:"installer_sha1,omitempty"` // sha1 правильного installer.jar
	PatchedSHA1   string   `json:"patched_sha1,omitempty"`   // sha1 правильного forge-*-client.jar
}

// loaderHintsFile: {"version":1,"loaders":{"forge":{"26.3-66.1.9":{...}},"neoforge":{...}}}
type loaderHintsFile struct {
	Version int                              `json:"version"`
	Loaders map[string]map[string]loaderHint `json:"loaders"`
}

// loaderHintFor повертає підказку для лоадера ("forge"/"neoforge") і повної
// версії (mc-loader). Мережа — з коротким таймаутом і дисковим кешем; будь-яка
// помилка = немає підказки (лаунчер працює як і раніше).
func (inst *Installer) loaderHintFor(name, fullVer string) (loaderHint, bool) {
	cachePath := filepath.Join(inst.baseDir, "cache", "loader-hints.json")
	var data []byte
	if LoaderHintsURL != "" {
		done := make(chan []byte, 1)
		go func() {
			b, err := inst.fetchURL(LoaderHintsURL)
			if err != nil {
				done <- nil
				return
			}
			done <- b
		}()
		select {
		case b := <-done:
			if len(b) > 0 {
				var probe loaderHintsFile
				if json.Unmarshal(b, &probe) == nil && probe.Loaders != nil {
					data = b
					_ = os.MkdirAll(filepath.Dir(cachePath), 0755)
					_ = os.WriteFile(cachePath, b, 0644)
				}
			}
		case <-time.After(6 * time.Second):
		}
	}
	if data == nil {
		data, _ = os.ReadFile(cachePath)
	}
	if len(data) == 0 {
		return loaderHint{}, false
	}
	var f loaderHintsFile
	if json.Unmarshal(data, &f) != nil {
		return loaderHint{}, false
	}
	h, ok := f.Loaders[name][fullVer]
	return h, ok
}

// forgeInstallerPath — шлях installer.jar у кеші (так само, як в installForge).
func (inst *Installer) forgeInstallerPath(mcVersion, loaderVersion string, neoforge bool) string {
	name := "forge"
	if neoforge {
		name = "neoforge"
	}
	return filepath.Join(inst.baseDir, "installers", name+"-"+mcVersion+"-"+loaderVersion+".jar")
}

// expectedPatchedSHA1 повертає очікуваний sha1 patched client.jar: спершу з
// самого installer.jar (install_profile.json → data.PATCHED_SHA), інакше з
// бази VPS. "" — перевірити нічим.
func (inst *Installer) expectedPatchedSHA1(mcVersion, loaderVersion string, neoforge bool) string {
	if zr, err := zip.OpenReader(inst.forgeInstallerPath(mcVersion, loaderVersion, neoforge)); err == nil {
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name != "install_profile.json" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				break
			}
			raw, _ := io.ReadAll(rc)
			rc.Close()
			var p installProfile
			if json.Unmarshal(raw, &p) == nil {
				if d, ok := p.Data["PATCHED_SHA"]; ok {
					v := strings.Trim(strings.TrimSpace(d.Client), "'")
					if len(v) == 40 && isHex(v) {
						return strings.ToLower(v)
					}
				}
			}
			break
		}
	}
	name := "forge"
	if neoforge {
		name = "neoforge"
	}
	if h, ok := inst.loaderHintFor(name, mcVersion+"-"+loaderVersion); ok {
		v := strings.ToLower(strings.TrimSpace(h.PatchedSHA1))
		if len(v) == 40 && isHex(v) {
			return v
		}
	}
	return ""
}

// loaderOKMarker — запис про успішну перевірку patched jar (щоб не хешувати
// 80+ МБ при кожному запуску).
type loaderOKMarker struct {
	SHA1    string `json:"sha1"`
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod_unix"`
}

func loaderMarkerPath(jar string) string { return jar + ".shaurma-ok" }

func writeLoaderMarker(jar, sha string) {
	fi, err := os.Stat(jar)
	if err != nil {
		return
	}
	b, _ := json.Marshal(loaderOKMarker{SHA1: sha, Size: fi.Size(), ModUnix: fi.ModTime().Unix()})
	_ = os.WriteFile(loaderMarkerPath(jar), b, 0644)
}

// verifyPatchedLoaderJar — true, якщо patched jar відповідає очікуваному sha1
// (або перевірити нічим). При розбіжності видаляє зіпсований jar і кеш binpatch,
// щоб install перебудував його з нуля.
func (inst *Installer) verifyPatchedLoaderJar(jar, mcVersion, loaderVersion string, neoforge bool) bool {
	fi, err := os.Stat(jar)
	if err != nil {
		return false
	}
	if raw, err := os.ReadFile(loaderMarkerPath(jar)); err == nil {
		var m loaderOKMarker
		if json.Unmarshal(raw, &m) == nil && m.SHA1 != "" && m.Size == fi.Size() && m.ModUnix == fi.ModTime().Unix() {
			return true
		}
	}
	want := inst.expectedPatchedSHA1(mcVersion, loaderVersion, neoforge)
	if want == "" {
		return true
	}
	got, err := sha1OfFile(jar)
	if err != nil {
		return false
	}
	if strings.EqualFold(got, want) {
		writeLoaderMarker(jar, got)
		return true
	}
	inst.log(fmt.Sprintf("Патчений jar лоадера пошкоджений/чужий (sha1 %s, очікується %s) — перебудовую.", got, want))
	_ = os.Remove(jar)
	_ = os.Remove(loaderMarkerPath(jar))
	_ = os.RemoveAll(inst.forgeCacheDir)
	return false
}

// purgeBadProcessorOutputs видаляє outputs, чий sha1 не збігається, та кеш
// binpatch — щоб зіпсований файл не прийняли за готовий при наступному запуску.
func (inst *Installer) purgeBadProcessorOutputs(outputs map[string]string, vars map[string]string) {
	for out, want := range outputs {
		p := inst.resolveProcValue(out, vars, nil)
		w := inst.resolveProcValue(want, vars, nil)
		if len(w) != 40 || !isHex(w) {
			continue
		}
		if got, err := sha1OfFile(p); err == nil && !strings.EqualFold(got, w) {
			inst.log(fmt.Sprintf("Видаляю зіпсований результат processor'а: %s", p))
			_ = os.Remove(p)
		}
	}
	_ = os.RemoveAll(inst.forgeCacheDir)
}

// recordProcessorOutputs ставить маркер «перевірено» на patched jar після
// успішного проходу processors.
func recordProcessorOutputs(outputsList []map[string]string, inst *Installer, vars map[string]string) {
	for _, outputs := range outputsList {
		for out, want := range outputs {
			p := inst.resolveProcValue(out, vars, nil)
			w := inst.resolveProcValue(want, vars, nil)
			if len(w) == 40 && isHex(w) && strings.HasSuffix(p, "-client.jar") {
				writeLoaderMarker(p, strings.ToLower(w))
			}
		}
	}
}
