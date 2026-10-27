// autobackup.go — періодичні (автоматичні) бекапи світів запущеного сервера.
//
// Раніше BackupConfig у Server існував лише як структура: жоден таймер його
// не читав, і в UI не було жодного способу його налаштувати — «авто-бекапи»
// були мертвим кодом. Тепер:
//   - кожен запущений сервер крутить свій невеликий цикл (autoBackupLoop);
//   - разом з інтервалом дотримується ліміт MaxPerDay (скільки авто-бекапів
//     на добу максимум) і автоочищення KeepCount/KeepDays;
//   - конфіг читається з диска на КОЖЕН тик, тож зміна налаштувань на
//     вкладці «Світи» діє одразу, без перезапуску сервера.
package servers

import (
	"shaurma-launcher-backend/internal/crashguard"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// autoBackupState — маленький реєстр фонових циклів (щоб не запустити два
// однакові цикли для одного сервера при повторному старті).
var autoBackupState = struct {
	mu sync.Mutex
	// last — коли цикл востаннє створив бекап (для інтервалу).
	last map[string]time.Time
	// active — чи цикл зараз крутиться.
	active map[string]bool
}{last: map[string]time.Time{}, active: map[string]bool{}}

// scheduleAutoBackup запускає фоновий цикл авто-бекапів для сервера.
// Викликається з prepareAndStart після успішного старту процесу.
func (a *API) scheduleAutoBackup(serverID string) {
	autoBackupState.mu.Lock()
	if autoBackupState.active[serverID] {
		autoBackupState.mu.Unlock()
		return
	}
	autoBackupState.active[serverID] = true
	// Перший бекап — через ПОВНИЙ інтервал після старту, а не одразу:
	// старт сервера і так вантажить диск, а щойно створений світ копіювати
	// нічого (гравці ще не встигли змінити нічого).
	autoBackupState.last[serverID] = time.Now()
	autoBackupState.mu.Unlock()

	{
		_f, _a0 := a.autoBackupLoop, serverID
		go func() {
			defer crashguard.Recover("a.autoBackupLoop")
			_f(_a0)
		}()
	}
}

// autoBackupLoop — фоновий цикл одного сервера. Завершується сам, щойно
// сервер припиняє працювати (або видалено зі стора).
func (a *API) autoBackupLoop(serverID string) {
	defer func() {
		autoBackupState.mu.Lock()
		delete(autoBackupState.active, serverID)
		delete(autoBackupState.last, serverID)
		autoBackupState.mu.Unlock()
	}()

	const tick = 20 * time.Second
	for {
		time.Sleep(tick)
		if !a.manager.IsRunning(serverID) {
			return
		}
		srv, ok := a.store.GetNoErr(serverID)
		if !ok {
			return
		}
		cfg := srv.AutoBackup
		if !cfg.Enabled || cfg.IntervalMin <= 0 {
			continue // вимкнено/ручний режим — просто чекаємо
		}
		interval := time.Duration(cfg.IntervalMin) * time.Minute

		autoBackupState.mu.Lock()
		last := autoBackupState.last[serverID]
		autoBackupState.mu.Unlock()
		if !last.IsZero() && time.Since(last) < interval {
			continue
		}

		autoBackupState.mu.Lock()
		autoBackupState.last[serverID] = time.Now()
		autoBackupState.mu.Unlock()

		a.runAutoBackups(srv, cfg)
	}
}

// runAutoBackups — власне «зробити бекапи + прибрати зайве».
// Позначка auto=true: копія створена не кнопкою, а таймером/зупинкою —
// UI позначає її значком «авто».
func (a *API) runAutoBackups(srv *Server, cfg BackupConfig) {
	worlds := autoBackupWorlds(srv, cfg)
	if len(worlds) == 0 {
		return
	}
	// Той самий м'ютекс, що й для ручного бекапу: таймер, «бекап при
	// зупинці» і «бекап при старті» не мають писати zip одночасно.
	mu := a.backupLock(srv.ID)
	mu.Lock()
	defer mu.Unlock()
	backupsDir := filepath.Join(srv.Dir, ".shaurma", "backups")

	for _, world := range worlds {
		// Ліміт на добу: рахуємо вже створені сьогодні копії САМОГО світу.
		if cfg.MaxPerDay > 0 && backupsCreatedToday(backupsDir, world) >= cfg.MaxPerDay {
			log.Printf("[servers] авто-бекап «%s» пропущено: ліміт %d на добу вичерпано", world, cfg.MaxPerDay)
			continue
		}
		// Прогрес летить у тe самий канал, що й для ручного бекапу: гравець
		// бачить у вкладці «Світи», що авто-копія зараз пишеться (а не
		// дивується раптовому zip у теці).
		path, err := CreateBackupWithProgress(srv.Dir, world, backupsDir, true, func(p BackupProgress) {
			a.notifyBackupProgress(srv.ID, p)
		})
		if err != nil {
			log.Printf("[servers] авто-бекап «%s» не вдався: %v", world, err)
			continue
		}
		log.Printf("[servers] авто-бекап «%s» створено: %s", world, filepath.Base(path))
		pruneBackups(backupsDir, world, cfg)
	}
}

// autoBackupWorlds — які світи копіювати: явний WorldDir з конфігу, якщо
// задано, інакше всі виявлені світи сервера (зазвичай це один «world» —
// Незер/Енд лежать усередині нього).
func autoBackupWorlds(srv *Server, cfg BackupConfig) []string {
	if w := filepath.Clean(cfg.WorldDir); w != "" && w != "." {
		// Шляхи всередині конфігу зберігаються як відносні до теки сервера.
		if _, err := os.Stat(filepath.Join(srv.Dir, w)); err == nil {
			return []string{filepath.Base(w)}
		}
	}
	var out []string
	for _, w := range ListWorlds(srv.Dir) {
		out = append(out, w.Name)
	}
	return out
}

// backupsCreatedToday — скільки бекапів світу вже створено СЬОГОДНІ
// (за локальною датою файлу). Використовується для ліміту MaxPerDay.
func backupsCreatedToday(backupsDir, world string) int {
	dayStart := startOfToday()
	n := 0
	for _, b := range ListBackups(backupsDir, world, backupsDir) {
		if !b.Created.Before(dayStart) {
			n++
		}
	}
	return n
}

func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// pruneBackups — автоочищення після створення копії:
//   - KeepCount: лишити N найновіших бекапів цього світу;
//   - KeepDays:  видалити все старіше за N днів.
//
// Обидва значення 0 = безліміт (нічого не видаляємо). pre-restore копії
// (сufікс «-pre-restore-») теж підпадають під ті самі правила — це звичайні
// бекапи, окремих винятків для них немає.
func pruneBackups(backupsDir, world string, cfg BackupConfig) {
	if cfg.KeepCount <= 0 && cfg.KeepDays <= 0 {
		return
	}
	all := ListBackups(backupsDir, world, backupsDir) // вже відсортовано: нові першими
	if len(all) == 0 {
		return
	}
	cutoff := time.Time{}
	if cfg.KeepDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -cfg.KeepDays)
	}
	for i, b := range all {
		drop := false
		if cfg.KeepCount > 0 && i >= cfg.KeepCount {
			drop = true
		}
		if !cutoff.IsZero() && b.Created.Before(cutoff) {
			drop = true
		}
		if drop {
			if err := DeleteBackup(backupsDir, b.ID); err == nil {
				log.Printf("[servers] авто-очищення бекапів «%s»: видалено %s", world, b.Name)
			}
		}
	}
}
