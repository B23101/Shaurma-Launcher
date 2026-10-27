package minecraft

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeOfflineVanilla кладе на диск мінімальний ванилла-профіль, щоб
// ensureVersionStart проходив повністю офлайн і швидко: downloads.client.url
// НЕПУСТИЙ (інакше спрацює захисний рефетч з Mojang-маніфесту — мережа), а
// sha1 порожній (jar перевіряється лише на існування, хеш не рахується).
func writeOfflineVanilla(t *testing.T, baseDir, ver string) {
	t.Helper()
	vdir := filepath.Join(baseDir, "versions", ver)
	if err := os.MkdirAll(vdir, 0755); err != nil {
		t.Fatalf("MkdirAll %s: %v", vdir, err)
	}
	meta := `{"id":"` + ver + `","type":"release","mainClass":"net.minecraft.client.main.Main",` +
		`"downloads":{"client":{"url":"http://127.0.0.1:9/client.jar","sha1":""}},` +
		`"libraries":[],"assets":"legacy","assetIndex":{"url":""}}`
	if err := os.WriteFile(filepath.Join(vdir, ver+".json"), []byte(meta), 0644); err != nil {
		t.Fatalf("write meta %s: %v", ver, err)
	}
	jar := filepath.Join(vdir, ver+".jar")
	if err := os.WriteFile(jar, []byte("dummy-jar"), 0644); err != nil {
		t.Fatalf("write jar %s: %v", ver, err)
	}
}

type metaLoaderResult struct {
	id  string
	err error
}

// TestInstallMetaLoaderNoSelfDeadlock — регресійний тест на ВІЧНИЙ ДЕДЛОК
// встановлення Fabric/Quilt. EnsureLoader тримає runMu (sync.Mutex), а
// installMetaLoader викликав публічний EnsureVersion, який брав runMu
// ПОВТОРНО тією ж горутиною. sync.Mutex НЕ реентрантний — горутина чекала,
// поки сама собі віддасть лок, і лаунчер «зависав намертво» рівно на
// «Встановлення лоадера...» після отримання fabric-профілю. Внутрішні шляхи
// тепер завантажують профіль/батька через ensureVersionStart (без займання
// runMu). Тест імітує умову: runMu тримає горутина «EnsureLoader», а
// installMetaLoader мусить завершитись — помилкою, але НЕ вічним блоком.
func TestInstallMetaLoaderNoSelfDeadlock(t *testing.T) {
	inst := NewInstaller(t.TempDir())
	writeOfflineVanilla(t, inst.baseDir, "1.20.1")

	// Локальний мета-сервер: той самий профіль, що й meta.fabricmc.net.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/versions/loader/1.20.1/0.16.9/profile/json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"fabric-loader-0.16.9-1.20.1","inheritsFrom":"1.20.1","mainClass":"net.fabricmc.loader.impl.launch.knot.KnotClient","libraries":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	old := fabricMetaURLs
	fabricMetaURLs = []string{srv.URL + "/v2/versions/loader/"}
	defer func() { fabricMetaURLs = old }()

	// Умова дедлока: runMu уже тримає ця горутина (так робить EnsureLoader).
	inst.runMu.Lock()
	defer inst.runMu.Unlock()

	done := make(chan metaLoaderResult, 1)
	go func() {
		id, err := inst.installMetaLoader(fabricMeta, "fabric", "1.20.1", "0.16.9")
		done <- metaLoaderResult{id: id, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("installMetaLoader мав завершитись БЕЗ дедлока, але повернув помилку: %v", res.err)
		}
		if res.id != "fabric-loader-0.16.9-1.20.1" {
			t.Fatalf("ID профілю = %q, очікував fabric-loader-0.16.9-1.20.1", res.id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("installMetaLoader завис намертво під утриманим runMu — повторний runMu.Lock() тією ж горутиною (дедлок EnsureLoader→EnsureVersion)")
	}
}

// TestEnsureVersionTakesRunMu — серіалізація публічного EnsureVersion
// має ЗБЕРЕГТИСЯ: поки runMu тримає інша горутина (фонове передзавантаження,
// інший EnsureLoader), EnsureVersion мусить блокуватись, а не проскочити паралельно.
func TestEnsureVersionTakesRunMu(t *testing.T) {
	inst := NewInstaller(t.TempDir())
	writeOfflineVanilla(t, inst.baseDir, "1.20.1")

	inst.runMu.Lock()
	started := make(chan struct{})
	released := make(chan struct{})
	go func() {
		close(started)
		_, _ = inst.EnsureVersion("1.20.1")
		close(released)
	}()

	<-started
	// EnsureVersion має чекати на runMu, а не виконатись одразу.
	select {
	case <-released:
		t.Fatal("EnsureVersion обійшов runMu — серіалізація втрачена")
	case <-time.After(150 * time.Millisecond):
	}

	inst.runMu.Unlock()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("EnsureVersion не продовжився після звільнення runMu")
	}
}