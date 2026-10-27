package modsource

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func setup(t *testing.T) (Store, string, string) {
	t.Helper()
	ResetReported()
	root := t.TempDir()
	return Store{Root: root, Name: NameMods}, filepath.Join(root, ".minecraft", "mods"), t.TempDir()
}

func TestFileSourceCopyUpdateRestore(t *testing.T) {
	st, mods, ext := setup(t)
	jar := filepath.Join(ext, "mymod.jar")
	write(t, jar, "v1")
	if _, err := st.Add(KindFile, jar, mods); err != nil {
		t.Fatal(err)
	}

	res, err := st.Sync(mods, nil)
	if err != nil || res.Restored != 1 {
		t.Fatalf("перше копіювання: %+v %v", res, err)
	}
	if read(t, filepath.Join(mods, "mymod.jar")) != "v1" {
		t.Fatal("копія не збігається")
	}

	// без змін — нічого не копіюється
	res, _ = st.Sync(mods, nil)
	if res.Changed() {
		t.Fatalf("зайва зміна: %+v", res)
	}

	// джерело оновлено (той самий розмір, інший вміст — хеш має спрацювати)
	write(t, jar, "v2")
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(jar, future, future)
	res, _ = st.Sync(mods, nil)
	if res.Copied != 1 || read(t, filepath.Join(mods, "mymod.jar")) != "v2" {
		t.Fatalf("оновлення: %+v", res)
	}

	// копію видалили з mods — відновлюється з джерела
	_ = os.Remove(filepath.Join(mods, "mymod.jar"))
	res, _ = st.Sync(mods, nil)
	if res.Restored != 1 || read(t, filepath.Join(mods, "mymod.jar")) != "v2" {
		t.Fatalf("відновлення: %+v", res)
	}

	// закріплений мод не може лишитись вимкненим
	_ = os.Rename(filepath.Join(mods, "mymod.jar"), filepath.Join(mods, "mymod.jar.disabled"))
	res, _ = st.Sync(mods, nil)
	if _, err := os.Stat(filepath.Join(mods, "mymod.jar.disabled")); err == nil {
		t.Fatal(".disabled-копія мала зникнути")
	}
	if res.Restored != 1 {
		t.Fatalf("після вимкнення: %+v", res)
	}
}

func TestMissingSourceReportedOnce(t *testing.T) {
	st, mods, ext := setup(t)
	jar := filepath.Join(ext, "a.jar")
	write(t, jar, "x")
	if _, err := st.Add(KindFile, jar, mods); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sync(mods, nil); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(jar)

	res, _ := st.Sync(mods, nil)
	if len(res.Missing) != 1 {
		t.Fatalf("очікували 1 повідомлення: %+v", res)
	}
	// копія в mods лишається
	if read(t, filepath.Join(mods, "a.jar")) != "x" {
		t.Fatal("копію не можна чіпати, коли джерела нема")
	}
	// вдруге за той самий запуск — мовчки
	res, _ = st.Sync(mods, nil)
	if len(res.Missing) != 0 {
		t.Fatalf("повторне повідомлення: %+v", res)
	}
	// «новий запуск»
	ResetReported()
	res, _ = st.Sync(mods, nil)
	if len(res.Missing) != 1 {
		t.Fatalf("після нового запуску: %+v", res)
	}
}

func TestFolderSourceMirrorAndExclusive(t *testing.T) {
	st, mods, ext := setup(t)
	src := filepath.Join(ext, "mods")
	write(t, filepath.Join(src, "a.jar"), "A")
	write(t, filepath.Join(src, "b.jar.disabled"), "B")
	write(t, filepath.Join(src, "readme.txt"), "ignored")
	// сторонній мод, що був у збірці до джерела
	write(t, filepath.Join(mods, "own.jar"), "OWN")

	if _, err := st.Add(KindFolder, src, mods); err != nil {
		t.Fatal(err)
	}
	// файлові джерела після теки заборонені
	other := filepath.Join(ext, "x.jar")
	write(t, other, "x")
	if _, err := st.Add(KindFile, other, mods); err == nil {
		t.Fatal("файл поруч із текою-джерелом має бути відхилений")
	}

	res, _ := st.Sync(mods, nil)
	if res.Restored != 2 || res.Moved != 1 {
		t.Fatalf("перше дзеркалювання: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(mods, "own.jar")); err == nil {
		t.Fatal("сторонній мод мав піти з mods")
	}
	if _, err := os.Stat(filepath.Join(mods, "readme.txt")); err == nil {
		t.Fatal("не-jar копіювати не треба")
	}
	if read(t, filepath.Join(mods, "b.jar.disabled")) != "B" {
		t.Fatal("стан disabled має копіюватись як є")
	}
	backups, _ := filepath.Glob(filepath.Join(st.preSourceDir(), "*", "own.jar"))
	if len(backups) != 1 {
		t.Fatalf("сторонній мод має бути збережений: %v", backups)
	}

	// у джерелі мод зник → зникає й у mods (бо його поставило саме джерело)
	_ = os.Remove(filepath.Join(src, "a.jar"))
	res, _ = st.Sync(mods, nil)
	if res.Removed != 1 {
		t.Fatalf("видалення: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(mods, "a.jar")); err == nil {
		t.Fatal("a.jar мав зникнути")
	}

	cfg, _ := st.Load()
	if _, locked := cfg.Locks("anything.jar"); !locked {
		t.Fatal("із текою-джерелом усе заблоковано")
	}
}

func TestAddValidation(t *testing.T) {
	st, mods, ext := setup(t)
	write(t, filepath.Join(mods, "in.jar"), "x")
	if _, err := st.Add(KindFile, filepath.Join(mods, "in.jar"), mods); err == nil {
		t.Fatal("джерело всередині mods має бути відхилене")
	}
	txt := filepath.Join(ext, "a.txt")
	write(t, txt, "x")
	if _, err := st.Add(KindFile, txt, mods); err == nil {
		t.Fatal("не-jar має бути відхилений")
	}
	if _, err := st.Add(KindFolder, mods, mods); err == nil {
		t.Fatal("тека-джерело = власні mods має бути відхилена")
	}
	jar := filepath.Join(ext, "a.jar")
	write(t, jar, "x")
	if _, err := st.Add(KindFile, jar, mods); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add(KindFile, jar, mods); err == nil {
		t.Fatal("дубль має бути відхилений")
	}
	if _, err := st.Add(KindFolder, ext, mods); err == nil {
		t.Fatal("тека при наявних файлових джерелах має бути відхилена")
	}
}

func TestRemoveKeepsCopiesAndUnlocks(t *testing.T) {
	st, mods, ext := setup(t)
	jar := filepath.Join(ext, "a.jar")
	write(t, jar, "x")
	src, _ := st.Add(KindFile, jar, mods)
	_, _ = st.Sync(mods, nil)
	cfg, _ := st.Load()
	if _, locked := cfg.Locks("a.jar"); !locked {
		t.Fatal("має бути заблоковано")
	}
	if err := st.Remove(src.ID); err != nil {
		t.Fatal(err)
	}
	cfg, _ = st.Load()
	if _, locked := cfg.Locks("a.jar"); locked {
		t.Fatal("після відкріплення блокування має зникнути")
	}
	if read(t, filepath.Join(mods, "a.jar")) != "x" {
		t.Fatal("відкріплення не повинно видаляти копію")
	}
}

func TestProgressReported(t *testing.T) {
	st, mods, ext := setup(t)
	for _, n := range []string{"a.jar", "b.jar", "c.jar"} {
		write(t, filepath.Join(ext, n), n)
		if _, err := st.Add(KindFile, filepath.Join(ext, n), mods); err != nil {
			t.Fatal(err)
		}
	}
	var last Progress
	calls := 0
	_, _ = st.Sync(mods, func(p Progress) { last = p; calls++ })
	if calls == 0 || last.Total != 3 || last.Percent != 100 {
		t.Fatalf("прогрес: %+v (викликів %d)", last, calls)
	}
}
