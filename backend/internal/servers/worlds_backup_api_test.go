// worlds_backup_api_test.go — регресійні тести бекапів світів ЧЕРЕЗ API-шар
// (не через низькорівневі функції).
//
// Навіщо саме через API: обидва баги, які тут закрито, жили саме в
// api.go — RestoreWorldBackup і DeleteWorldBackup отримували порожню
// backupsDir, і шлях склеювався ВІДНОСНО робочої теки лаунчера
// (DeleteWorldBackup ще й дивився в глобальну теку даних). Виклик
// RestoreBackup/DeleteBackup напряму з теки сервера ці баги не ловить —
// саме тому попередні тести їх і не зловили.
package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestWorldEnv створює сервер із світом «world» (level.dat + region/).
func newTestWorldEnv(t *testing.T) (*API, *Server) {
	t.Helper()
	dir := t.TempDir()
	api := NewAPI(dir, nil, nil)

	srv := &Server{
		ID:        "srv1",
		Name:      "Тестовий",
		Dir:       filepath.Join(dir, "servers", "srv1"),
		Core:      CorePaper,
		MCVersion: "1.21.1",
		Port:      25565,
	}
	for _, d := range []string{
		filepath.Join(srv.Dir, ".shaurma"),
		filepath.Join(srv.Dir, "world", "region"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(srv.Dir, "world", "level.dat"), []byte("ORIGINAL"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srv.Dir, "world", "region", "r.0.0.mca"), []byte("REGION"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srv.Dir, "server.properties"), []byte("level-name=world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := api.store.Save(srv); err != nil {
		t.Fatal(err)
	}
	return api, srv
}

// TestWorldBackupRoundTripViaAPI — create → list → restore → delete через API.
func TestWorldBackupRoundTripViaAPI(t *testing.T) {
	api, srv := newTestWorldEnv(t)

	// 1. Створення бекапу + події прогресу.
	var events []BackupProgress
	api.SetBackupProgressCallback(func(_ string, p BackupProgress) { events = append(events, p) })

	path, err := api.CreateWorldBackup(srv.ID, "world")
	if err != nil {
		t.Fatalf("CreateWorldBackup: %v", err)
	}
	wantDir := filepath.Join(srv.Dir, ".shaurma", "backups")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("бекап створено не в теці сервера: %s (очікували %s)", filepath.Dir(path), wantDir)
	}
	if len(events) == 0 {
		t.Fatal("жодної події прогресу не надіслано — UI не побачить прогрес")
	}
	if got := events[0].Stage; got != "packing" {
		t.Errorf("перша подія: stage=%q, очікували packing", got)
	}
	if events[0].Files != 2 {
		t.Errorf("файлів у світі: %d, очікували 2", events[0].Files)
	}
	last := events[len(events)-1]
	if last.Stage != "done" || last.Percent != 100 {
		t.Errorf("остання подія: stage=%q percent=%d, очікували done/100", last.Stage, last.Percent)
	}
	if last.Path != path {
		t.Errorf("остання подія вказує на %q, а бекап лежить у %q", last.Path, path)
	}

	// 2. Список: світ визначається правильно, метафайл читається.
	backups, err := api.ListWorldBackups(srv.ID, "world")
	if err != nil {
		t.Fatalf("ListWorldBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("бекапів: %d, очікували 1", len(backups))
	}
	b := backups[0]
	if b.World != "world" {
		t.Errorf("світ бекапу: %q, очікували world", b.World)
	}
	if b.Files != 2 {
		t.Errorf("файлів у метафайлі: %d, очікували 2", b.Files)
	}
	if b.Auto {
		t.Error("ручний бекап позначено як auto")
	}

	// 3. Псуємо світ і відновлюємо — саме цей шлях раніше завжди падав
	//    («бекап не знайдено»), бо шукав zip у робочій теці лаунчера.
	if err := os.WriteFile(filepath.Join(srv.Dir, "world", "level.dat"), []byte("CORRUPT"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := api.RestoreWorldBackup(srv.ID, "world", b.ID); err != nil {
		t.Fatalf("RestoreWorldBackup: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(srv.Dir, "world", "level.dat"))
	if err != nil {
		t.Fatalf("level.dat після відновлення: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("level.dat після відновлення = %q, очікували ORIGINAL", string(got))
	}
	if _, err := os.Stat(filepath.Join(srv.Dir, "world", "region", "r.0.0.mca")); err != nil {
		t.Errorf("region/ не відновлено: %v", err)
	}

	// 4. Видалення має реально прибирати файл з диска (регресія: раніше
	//    дивилось у глобальну теку даних лаунчера і «успішно» нічого не робило).
	zipPath := filepath.Join(wantDir, b.ID+".zip")
	if err := api.DeleteWorldBackup(srv.ID, "world", b.ID); err != nil {
		t.Fatalf("DeleteWorldBackup: %v", err)
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Errorf("zip бекапу лишився на диску: %s", zipPath)
	}
	if _, err := os.Stat(filepath.Join(wantDir, b.ID+".meta.json")); !os.IsNotExist(err) {
		t.Errorf("метафайл бекапу лишився на диску")
	}
}

// TestDeleteWorldBackupMissingDoesNotFailSilently — видалення неіснуючого
// бекапу має бути помилкою, а не тихим «успіхом» (інакше UI показує
// «видалено», а файл лишається).
func TestDeleteWorldBackupMissing(t *testing.T) {
	api, srv := newTestWorldEnv(t)
	if err := api.DeleteWorldBackup(srv.ID, "world", "world-20260101-000000"); err == nil {
		t.Fatal("видалення неіснуючого бекапу не повернуло помилку")
	}
}

// TestWorldBackupSameSecondNoOverwrite — два бекапи одного світу в одну
// секунду не мають перезаписувати один одного (ім'я zip має секундну
// точність, тож без суфікса другий архів знищив би перший).
func TestWorldBackupSameSecondNoOverwrite(t *testing.T) {
	api, srv := newTestWorldEnv(t)

	p1, err := api.CreateWorldBackup(srv.ID, "world")
	if err != nil {
		t.Fatalf("перший бекап: %v", err)
	}
	p2, err := api.CreateWorldBackup(srv.ID, "world")
	if err != nil {
		t.Fatalf("другий бекап: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("два бекапи лягли в один файл: %s", p1)
	}
	backups, err := api.ListWorldBackups(srv.ID, "world")
	if err != nil {
		t.Fatalf("ListWorldBackups: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("бекапів: %d, очікували 2", len(backups))
	}
	for _, b := range backups {
		if b.World != "world" {
			t.Errorf("світ бекапу %s = %q, очікували world", b.ID, b.World)
		}
		if b.Files != 2 {
			t.Errorf("файлів у %s: %d, очікували 2", b.ID, b.Files)
		}
	}
}

// TestRestoreWorldBackupUnknownID — невідомий бекап = зрозуміла помилка.
func TestRestoreWorldBackupUnknownID(t *testing.T) {
	api, srv := newTestWorldEnv(t)
	err := api.RestoreWorldBackup(srv.ID, "world", "world-20260101-000000")
	if err == nil || !strings.Contains(err.Error(), "не знайдено") {
		t.Fatalf("очікували «не знайдено», отримали %v", err)
	}
}

// TestWorldFromBackupID — ім'я світу з ідентифікатора бекапу (у назві світу
// може бути дефіс, а pre-restore/imported — це суфікси, а не частина назви).
func TestWorldFromBackupID(t *testing.T) {
	cases := map[string]string{
		"world-20260823-181035":                "world",
		"my-world-20260823-181035":             "my-world",
		"world-pre-restore-20260823-181035":    "world",
		"my-world-pre-restore-20260823-181035": "my-world",
		"world-imported-20260823-181035":       "world",
		// Числовий суфікс — колізія імен в одну секунду (див. CreateBackupWithProgress).
		"world-20260823-181035-1":     "world",
		"my-world-20260823-181035-12": "my-world",
		"без-дати":                    "fallback",
	}
	for id, want := range cases {
		if got := worldFromBackupID(id, "fallback"); got != want {
			t.Errorf("worldFromBackupID(%q) = %q, очікували %q", id, got, want)
		}
	}
}

// TestPlayerJoinLeaveFromLog — ніки у сповіщеннях про вхід/вихід.
func TestPlayerJoinLeaveFromLog(t *testing.T) {
	const srv = "players-test"
	ClearOnlinePlayers(srv)
	defer ClearOnlinePlayers(srv)

	// Вхід (ванільний формат з префіксом часу).
	ev, ok := UpdatePlayersFromLog(srv, "[12:00:00] [Server thread/INFO]: Steve joined the game")
	if !ok || ev.Action != "join" || ev.Name != "Steve" || ev.Count != 1 {
		t.Fatalf("join: ok=%v ev=%+v", ok, ev)
	}
	if len(ev.List) != 1 || ev.List[0] != "Steve" {
		t.Errorf("список після входу: %v", ev.List)
	}

	// Другий гравець.
	if _, ok := UpdatePlayersFromLog(srv, "[12:00:05] [Server thread/INFO]: Alex_99 joined the game"); !ok {
		t.Fatal("другий join не розпізнано")
	}
	if names := OnlinePlayerNames(srv); len(names) != 2 {
		t.Fatalf("онлайн: %v, очікували 2", names)
	}

	// Вихід.
	ev, ok = UpdatePlayersFromLog(srv, "[12:01:00] [Server thread/INFO]: Steve left the game")
	if !ok || ev.Action != "leave" || ev.Name != "Steve" || ev.Count != 1 {
		t.Fatalf("leave: ok=%v ev=%+v", ok, ev)
	}

	// Обрив зв'язку — теж вихід, з причиною.
	if _, ok := UpdatePlayersFromLog(srv, "[12:02:00] [Server thread/INFO]: Alex_99 lost connection: Timed out"); !ok {
		t.Fatal("lost connection не розпізнано як вихід")
	}
	if names := OnlinePlayerNames(srv); len(names) != 0 {
		t.Errorf("після виходів онлайн: %v, очікували порожньо", names)
	}

	// Повторний вихід того, кого немає, не має плодити подію.
	if _, ok := UpdatePlayersFromLog(srv, "[12:03:00] Steve left the game"); ok {
		t.Error("подія «вийшов» для гравця, якого немає в реєстрі")
	}

	// Velocity-формат.
	if ev, ok := UpdatePlayersFromLog(srv, "[12:04:00] [connected player] Steve (/127.0.0.1:5000) has connected"); !ok || ev.Action != "join" {
		t.Errorf("velocity join: ok=%v ev=%+v", ok, ev)
	}
	if ev, ok := UpdatePlayersFromLog(srv, "[12:05:00] [connected player] Steve (/127.0.0.1:5000) has disconnected"); !ok || ev.Action != "leave" {
		t.Errorf("velocity leave: ok=%v ev=%+v", ok, ev)
	}

	// Рядок без подій не має нічого повертати.
	if _, ok := UpdatePlayersFromLog(srv, "[12:06:00] [Server thread/INFO]: Preparing spawn area: 42%"); ok {
		t.Error("звичайний рядок логу розпізнано як подію гравця")
	}
}

// TestPlayerListResync — /list відновлює список після перезапуску лаунчера,
// не породжуючи при цьому сповіщень про вхід.
func TestPlayerListResync(t *testing.T) {
	const srv = "players-resync"
	ClearOnlinePlayers(srv)
	defer ClearOnlinePlayers(srv)

	if _, ok := UpdatePlayersFromLog(srv, "[12:00:00] [Server thread/INFO]: There are 2 of a max of 20 players online: Steve, Alex"); ok {
		t.Error("/list не має породжувати подію входу")
	}
	names := OnlinePlayerNames(srv)
	if len(names) != 2 {
		t.Fatalf("після ресинку: %v, очікували 2", names)
	}

	// Нове /list, де одного вже немає — список має звузитись.
	UpdatePlayersFromLog(srv, "[12:01:00] [Server thread/INFO]: There are 1 of a max of 20 players online: Steve")
	if names := OnlinePlayerNames(srv); len(names) != 1 || names[0] != "Steve" {
		t.Errorf("після ресинку: %v, очікували [Steve]", names)
	}

	// Порожній список.
	UpdatePlayersFromLog(srv, "[12:02:00] [Server thread/INFO]: There are 0 of a max of 20 players online:")
	if names := OnlinePlayerNames(srv); len(names) != 0 {
		t.Errorf("після спорожнення: %v", names)
	}
}

// TestClearOnlinePlayers — зупинка сервера чистить реєстр.
func TestClearOnlinePlayers(t *testing.T) {
	const srv = "players-clear"
	ClearOnlinePlayers(srv)
	UpdatePlayersFromLog(srv, "Steve joined the game")
	if len(OnlinePlayerNames(srv)) != 1 {
		t.Fatal("гравець не додався")
	}
	ClearOnlinePlayers(srv)
	if len(OnlinePlayerNames(srv)) != 0 {
		t.Error("реєстр не очистився")
	}
}
