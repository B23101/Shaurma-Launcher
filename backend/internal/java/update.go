package java

// Автооновлення вбудованої Java.
//
// Раніше Java ставилась один раз і більше ніколи не мінялась: гравці роками
// сиділи на старих збірках 17/21 без виправлень безпеки. Тепер після
// встановлення поруч із Java лежить мітка .shaurma-runtime.json (SHA1
// пакетного маніфесту Mojang + версія). Перед запуском гри, не частіше
// разу на добу, лаунчер звіряє її з маніфестом Mojang і, якщо там новіша
// збірка, качає її поруч (java-<major>.update) і підміняє теку.
//
// Нема інтернету / Mojang недоступний / Java зайнята іншою запущеною
// грою — граємо на поточній Java, спробуємо наступного разу. Встановлення
// без мітки (старі версії лаунчера) вважаються застарілими й оновлюються
// один раз. Java, яку користувач вказав сам (шлях до .exe), сюди не
// потрапляє: лаунчер оновлює лише теки, які сам і заповнює.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	runtimeMarkerName   = ".shaurma-runtime.json"
	updateCheckInterval = 24 * time.Hour
	updateCheckTimeout  = 20 * time.Second
)

type runtimeMarker struct {
	Component string    `json:"component"`
	Version   string    `json:"version"`
	SHA1      string    `json:"sha1"`
	CheckedAt time.Time `json:"checkedAt"`
}

func readRuntimeMarker(dir string) (runtimeMarker, bool) {
	var m runtimeMarker
	data, err := os.ReadFile(filepath.Join(dir, runtimeMarkerName))
	if err != nil || json.Unmarshal(data, &m) != nil || m.SHA1 == "" {
		return runtimeMarker{}, false
	}
	return m, true
}

func writeRuntimeMarker(dir string, m runtimeMarker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, runtimeMarkerName), data, 0644)
}

// checkDue — чи пора знову питати Mojang (раз на добу; годинник, що
// втік у майбутнє, теж вважаємо приводом перевірити).
func checkDue(m runtimeMarker, now time.Time) bool {
	return now.Sub(m.CheckedAt) >= updateCheckInterval || m.CheckedAt.After(now.Add(time.Hour))
}

// checkUpdate повертає актуальний запис маніфесту і чи треба оновлювати
// Java у dir. Будь-яка мережева помилка = «не треба» (граємо на поточній).
func (j *Installer) checkUpdate(dir, component string, major int) (runtimeEntry, bool) {
	m, ok := readRuntimeMarker(dir)
	known := ok && m.Component == component
	if known && !checkDue(m, time.Now()) {
		return runtimeEntry{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	entry, err := j.resolveRuntime(ctx, component)
	if err != nil {
		j.status(fmt.Sprintf("Не вдалося перевірити оновлення Java %d: %v", major, err))
		return runtimeEntry{}, false
	}
	if known && m.SHA1 == entry.SHA1 {
		m.CheckedAt = time.Now()
		_ = writeRuntimeMarker(dir, m)
		return entry, false
	}
	return entry, true
}

// refreshMajorDir — Java потрібного мажора вже є у java-<major>/:
// оновлюємо її на місці, якщо Mojang випустив новішу збірку.
func (j *Installer) refreshMajorDir(component string, major int) (string, error) {
	dir := filepath.Join(j.dir, fmt.Sprintf("java-%d", major))
	entry, need := j.checkUpdate(dir, component, major)
	if !need {
		return j.MajorExePath(major), nil
	}
	return j.updateInto(component, major, entry, j.MajorExePath(major))
}

// refreshRoot — потрібний мажор лежить у корені javaDir (стара розкладка).
// Корінь не чіпаємо (там підтеки інших мажорів): свіжа Java лягає у
// java-<major>/, і наступні запуски беруть уже її.
func (j *Installer) refreshRoot(component string, major int) (string, error) {
	entry, need := j.checkUpdate(j.dir, component, major)
	if !need {
		return j.ExePath(), nil
	}
	return j.updateInto(component, major, entry, j.ExePath())
}

// updateInto качає entry у java-<major>.update і підміняє нею java-<major>/.
// Невдача (крім «Зупинити») — запуск на fallbackExe.
func (j *Installer) updateInto(component string, major int, entry runtimeEntry, fallbackExe string) (string, error) {
	dir := filepath.Join(j.dir, fmt.Sprintf("java-%d", major))
	staging := dir + ".update"
	_ = os.RemoveAll(staging)

	j.status(fmt.Sprintf("Оновлення Java %d до %s...", major, entry.Version))
	if err := j.installFromEntry(component, major, staging, entry); err != nil {
		_ = os.RemoveAll(staging)
		if errors.Is(err, ErrInstallCancelled) {
			return "", err
		}
		j.status(fmt.Sprintf("Оновити Java %d не вдалося, запуск на поточній: %v", major, err))
		return fallbackExe, nil
	}
	if err := swapDir(dir, staging); err != nil {
		_ = os.RemoveAll(staging)
		j.status(fmt.Sprintf("Java %d зараз використовується, оновимо під час наступного запуску: %v", major, err))
		return fallbackExe, nil
	}
	return j.MajorExePath(major), nil
}

// swapDir атомарно (наскільки дозволяє ФС) ставить staging на місце dir.
// Якщо dir зайнята (Windows не дає перейменувати теку з запущеним
// java.exe) — повертає помилку і dir лишається як була.
func swapDir(dir, staging string) error {
	old := dir + ".old"
	_ = os.RemoveAll(old)
	hadOld := false
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
		hadOld = true
	}
	if err := os.Rename(staging, dir); err != nil {
		if hadOld {
			_ = os.Rename(old, dir)
		}
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}
