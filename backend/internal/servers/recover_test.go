package servers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHelperSleep — допоміжний процес для тестів відновлення: тестовий
// бінарник, перезапущений сам у себе, просто висить, поки тест не вб'є
// його. Потрібен, щоб мати РЕАЛЬНО живий процес із перевірюваним часом
// створення (заспавнити «фейковий java» неможливо, тож перевірка образу
// в тесті інжектиться як заглушка в adoptRecord).
func TestHelperSleep(t *testing.T) {
	if os.Getenv("SERVERS_TEST_SLEEP_HELPER") != "1" {
		return
	}
	time.Sleep(60 * time.Second)
}

// spawnHelper запускає допоміжний «живий» процес і повертає його cmd.
func spawnHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperSleep")
	cmd.Env = append(os.Environ(), "SERVERS_TEST_SLEEP_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("старт допоміжного процесу: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}

// Тестовий запис: пишеться так само, як у Start(), і читається так само,
// як під час старту лаунчера.
func TestRuntimeRecordRoundtrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := readRuntimeRecord(dir); ok {
		t.Fatal("без файлу запис існує")
	}
	want := runtimeRecord{PID: 4321, CreatedNs: 123456789, StartedAt: 999, Core: "paper", Port: 25566, Java: "j"}
	writeRuntimeRecord(dir, want)
	got, ok := readRuntimeRecord(dir)
	if !ok {
		t.Fatal("запис не прочитався")
	}
	if got != want {
		t.Fatalf("roundtrip: отримали %+v, очікували %+v", got, want)
	}
	removeRuntimeRecord(dir)
	if _, ok := readRuntimeRecord(dir); ok {
		t.Fatal("запис не видалився")
	}
	if !filepath.IsAbs(runtimeRecordPath(dir)) {
		t.Fatal("шлях до runtime-файлу має бути абсолютним")
	}
}

// ГОЛОВНИЙ сценарій: лаунчер «крашиться», java лишається живим. Новий
// лаунчер має усиновити процес: сервер показується запущеним (не «порт
// зайнятий»), статус — running/recovered, статистика стартує, а коли
// процес зрештою завершується — усе коректно фіналізується.
func TestAdoptLiveOrphanedServer(t *testing.T) {
	dir := t.TempDir()
	helper := spawnHelper(t)
	snap := snapshotProcess(helper.Process.Pid)
	if !snap.Alive {
		t.Skip("не вдалося отримати знімок процесу в цьому середовищі")
	}

	srv := &Server{ID: "srv-adopt", Dir: dir, Core: "paper", Port: 25569, RAMMB: 1024}
	rec := runtimeRecord{
		PID: helper.Process.Pid, CreatedNs: snap.CreateNs,
		StartedAt: time.Now().Add(-time.Hour).UnixMilli(),
		Core:      "paper", Port: 25569,
	}
	writeRuntimeRecord(dir, rec)

	m := NewManager()
	// У тесті «java» ніхто не запускає — перевірку образу інжектимо.
	if !m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return true }) {
		t.Fatal("живий процес не усиновлено")
	}

	if !m.IsRunning(srv.ID) {
		t.Fatal("після відновлення сервер має бути «запущеним» — саме це прибирає помилку «порт зайнятий»")
	}
	st := m.GetStatus(srv.ID)
	if st.Status != "running" || !st.Recovered || st.PID != rec.PID {
		t.Fatalf("статус: %+v, очікували running/recovered/PID", st)
	}
	uptime := st.UptimeSec
	if uptime < 3600 {
		t.Fatalf("uptime має рахуватись від ПОЧАТКОВОГО старту сервера, отримали %dс", uptime)
	}

	// Консоль отримує SYSTEM-рядок про відновлення (pipe утрачено, тож
	// більше нічого нового там не буде).
	var sawLine bool
	for _, l := range m.GetServerConsoleSnapshot(srv.ID) {
		if strings.Contains(l.Text, "Керування відновлено") {
			sawLine = true
		}
	}
	if !sawLine {
		t.Fatal("у консолі немає SYSTEM-рядка про відновлення керування")
	}

	// Команди консолі чесно кажуть, що канал утрачено (не паніка на nil).
	if err := m.SendCommand(srv.ID, "list"); err == nil {
		t.Fatal("команда в консоль відновленого процесу не може вдатися — stdin утрачено")
	}

	// Повторне усиновлення того самого сервера не має дублювати процес.
	if m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return true }) {
		t.Fatal("повторне усиновлення живого запису має бути no-op")
	}

	// Статистика вмикається (GC-лог і пінг порту не залежать від pipe).
	if _, err := os.Stat(GCLogPath(dir)); err != nil {
		// Файл створюється не одразу колектором — допускаємо відсутність,
		// але колекція статистики мусить створити стейт сервера.
		if GetStats(srv.ID).PID != rec.PID {
			t.Fatalf("статистика не підхопила сервер: %+v", GetStats(srv.ID))
		}
	}

	// Процес завершився → watchAdopted має фіналізувати сервер.
	_ = helper.Process.Kill()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !m.IsRunning(srv.ID) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if m.IsRunning(srv.ID) {
		t.Fatal("після завершення усиновленого процесу сервер досі «запущений»")
	}
	if _, ok := readRuntimeRecord(dir); ok {
		t.Fatal("runtime-запис мав бути прибрано після завершення процесу")
	}
	// Другий раз уже нічого усиновувати.
	if m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return true }) {
		t.Fatal("мертвий процес не можна усиновити")
	}
	if _, ok := readRuntimeRecord(dir); ok {
		t.Fatal("застарілий запис мав бути прибраний")
	}
}

// Запис із «не тим» процесом (PID перевикористано) — не усиновлюємо:
// це саме та ситуація, коли чужий процес став би «нашим сервером».
func TestAdoptRejectsForeignProcess(t *testing.T) {
	dir := t.TempDir()
	helper := spawnHelper(t)
	rec := runtimeRecord{PID: helper.Process.Pid, CreatedNs: 1, StartedAt: time.Now().UnixMilli(), Core: "paper", Port: 25565}
	writeRuntimeRecord(dir, rec)

	srv := &Server{ID: "srv-foreign", Dir: dir, Core: "paper", Port: 25565}
	m := NewManager()
	if m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return true }) {
		t.Fatal("процес із НЕзбіглим часом створення не можна усиновлювати")
	}
	if m.IsRunning(srv.ID) {
		t.Fatal("сервер має лишитись незапущеним")
	}
	if _, ok := readRuntimeRecord(dir); ok {
		t.Fatal("застарілий запис мав бути прибраний")
	}

	// Запис із «не java» процесом — теж відмова (чужий додаток не чіпаємо).
	snap := snapshotProcess(helper.Process.Pid)
	if snap.Alive && snap.CreateNs != 0 {
		rec.CreatedNs = snap.CreateNs
		writeRuntimeRecord(dir, rec)
		if m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return false }) {
			t.Fatal("не-java процес не можна усиновлювати")
		}
		if _, ok := readRuntimeRecord(dir); ok {
			t.Fatal("запис про чужий процес мав бути прибраний")
		}
	}
}

// Один раз можна усиновити лише одного разу — «сервер уже запущено».
func TestAdoptDoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	helper := spawnHelper(t)
	snap := snapshotProcess(helper.Process.Pid)
	srv := &Server{ID: "srv-dup", Dir: dir, Core: "paper"}
	rec := runtimeRecord{PID: helper.Process.Pid, CreatedNs: snap.CreateNs, StartedAt: time.Now().UnixMilli(), Core: "paper"}
	writeRuntimeRecord(dir, rec)

	m := NewManager()
	if !m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return true }) {
		t.Fatal("перше усиновлення")
	}
	if m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return true }) {
		t.Fatal("друге усиновлення того самого сервера має бути no-op")
	}
	// Stop має завершитись (kill за PID), не зависнувши на втраченому stdin.
	done := make(chan error, 1)
	go func() { done <- m.Stop(srv.ID, true) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop відновленого сервера: %v", err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("Stop завис: для відновленого процесу graceful-канал недоступний, мав спрацювати kill")
	}
}

