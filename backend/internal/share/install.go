package share

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Встановлення прийнятого (перенесення files/ у теку збірки/сервера + реєстрація в
// лаунчері) — не атомарне: краш між кроками лишав або «збірку» без файлів, або файли
// без збірки. Тому перед перенесенням у stage пишеться маркер install.json, і після
// перезапуску RecoverInstall доводить справу до одного з двох чистих станів:
//   - реєстрацію завершено → stage більше не потрібен (Registered=true);
//   - ні → файли повертаються в stage, напівготова тека призначення видаляється,
//     і встановлення можна повторити (журнал «complete» лишається).

// InstallMarker — що саме встановлювалось.
type InstallMarker struct {
	// Dst — тека призначення (абсолютний шлях).
	Dst string `json:"dst"`
	// ResultID — id збірки/сервера, під яким реєструємо (відомий до перенесення).
	ResultID string `json:"resultId,omitempty"`
	// Registered — реєстрація в лаунчері завершена; лишилось прибрати stage.
	Registered bool `json:"registered,omitempty"`
}

func installPath(stage string) string { return filepath.Join(stage, "install.json") }

// BeginInstall пише маркер перед перенесенням файлів.
func BeginInstall(stage string, m InstallMarker) error {
	if m.Dst == "" || !filepath.IsAbs(m.Dst) {
		return errors.New("некоректна тека призначення")
	}
	return writeJSONAtomic(installPath(stage), m)
}

// MarkInstalled — реєстрація пройшла; після цього stage можна видаляти.
func MarkInstalled(stage string, m InstallMarker) error {
	m.Registered = true
	return writeJSONAtomic(installPath(stage), m)
}

// ReadInstall — маркер незавершеного встановлення (ok=false, якщо його немає).
func ReadInstall(stage string) (InstallMarker, bool) {
	raw, err := os.ReadFile(installPath(stage))
	if err != nil {
		return InstallMarker{}, false
	}
	var m InstallMarker
	if json.Unmarshal(raw, &m) != nil || m.Dst == "" || !filepath.IsAbs(m.Dst) {
		return InstallMarker{}, false
	}
	return m, true
}

// ClearInstall прибирає маркер (встановлення відкочено або не почалось).
func ClearInstall(stage string) { _ = os.Remove(installPath(stage)) }

// RecoverInstall — після перезапуску. registered(m) каже, чи лаунчер уже знає збірку/сервер
// m.ResultID (реєстрація могла пройти, а маркер не встиг оновитись). Повертає:
//
//	done=true  — встановлення завершене, stage треба прибрати, результат m.ResultID;
//	done=false — файли (знову) в stage, маркер прибрано; err — якщо відкат не вдався.
func RecoverInstall(stage string, registered func(InstallMarker) bool) (m InstallMarker, done bool, err error) {
	m, ok := ReadInstall(stage)
	if !ok {
		return m, false, nil
	}
	if m.Registered || (m.ResultID != "" && registered != nil && registered(m)) {
		return m, true, nil
	}
	files := FilesDir(stage)
	_, srcErr := os.Stat(files)
	_, dstErr := os.Stat(m.Dst)
	switch {
	case srcErr == nil && dstErr == nil:
		// Обірване копіювання між дисками: оригінали ще в stage, у призначенні — уламок.
		if err := os.RemoveAll(m.Dst); err != nil {
			return m, false, err
		}
	case srcErr != nil && dstErr == nil:
		// Перенесення відбулось, реєстрація — ні: повертаємо файли назад.
		if err := MoveTree(m.Dst, files); err != nil {
			return m, false, err
		}
	}
	ClearInstall(stage)
	return m, false, nil
}

func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
