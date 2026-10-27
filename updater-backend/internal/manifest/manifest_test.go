package manifest

// manifest_test.go — регресійні тести Diff (вибір компонентів для качки).
//
// Поведінка, яку захищаємо: при бампі версії качаються ЛИШЕ реально
// змінені компоненти (sha256/розмір), а не всі підряд. Раніше Diff
// повертав remote.Components цілком — кожне оновлення перекачувало
// frontend.zip та інші незмінені файли («усе видаляє і качає, навіть
// якщо там нічого не оновилося»).

import (
	"runtime"
	"testing"
)

func comp(path, sha string, size int64) Component {
	return Component{Path: path, SHA256: sha, Size: size, URL: "/launcher/" + path}
}

func fullRemote() *Manifest {
	return &Manifest{
		Version: "2.1.0",
		Components: []Component{
			comp("backend.dll", "aaa", 100),
			comp("updater.dll", "bbb", 200),
			comp("frontend.zip", "ccc", 300),
		},
	}
}

// TestDiffOnlyChanged — версія новіша, змінився лише backend.dll:
// качається тільки він.
func TestDiffOnlyChanged(t *testing.T) {
	local := &Manifest{
		Version: "2.0.0",
		Components: []Component{
			comp("backend.dll", "OLD", 100),
			comp("updater.dll", "bbb", 200),
			comp("frontend.zip", "ccc", 300),
		},
	}
	got := Diff(local, fullRemote())
	if len(got) != 1 || got[0].Path != "backend.dll" {
		t.Fatalf("очікувано лише backend.dll, отримано: %+v", got)
	}
}

// TestDiffNoChanges — бамп версії без зміни вмісту (напр. тільки
// releaseNotes): качати нічого.
func TestDiffNoChanges(t *testing.T) {
	local := &Manifest{
		Version: "2.0.9",
		Components: []Component{
			comp("backend.dll", "aaa", 100),
			comp("updater.dll", "bbb", 200),
			comp("frontend.zip", "ccc", 300),
		},
	}
	if got := Diff(local, fullRemote()); len(got) != 0 {
		t.Fatalf("без змін вмісту качка не потрібна, отримано: %+v", got)
	}
}

// TestDiffSameOrOlderVersion — версія та сама або старіша: завжди нічого.
func TestDiffSameOrOlderVersion(t *testing.T) {
	same := &Manifest{Version: "2.1.0", Components: fullRemote().Components}
	if got := Diff(same, fullRemote()); len(got) != 0 {
		t.Fatalf("та сама версія: очікувано 0, отримано %+v", got)
	}
	older := &Manifest{Version: "2.2.0", Components: fullRemote().Components}
	if got := Diff(older, fullRemote()); len(got) != 0 {
		t.Fatalf("remote старіша: очікувано 0, отримано %+v", got)
	}
}

// TestDiffNewComponentAdded — компонент з'явився в remote: качається.
func TestDiffNewComponentAdded(t *testing.T) {
	local := &Manifest{
		Version: "2.0.0",
		Components: []Component{
			comp("backend.dll", "aaa", 100),
		},
	}
	got := Diff(local, fullRemote())
	if len(got) != 2 {
		t.Fatalf("очікувано 2 нові компоненти, отримано: %+v", got)
	}
}

// TestDiffEmptyLocal — локального маніфесту немає (перший запуск):
// качається ВСЕ.
func TestDiffEmptyLocal(t *testing.T) {
	got := Diff(&Manifest{}, fullRemote())
	if len(got) != len(fullRemote().Components) {
		t.Fatalf("перший запуск: очікувано всі %d, отримано %d", len(fullRemote().Components), len(got))
	}
}

// TestDiffSizeChanged — розмір відрізняється (sha той самий): качається.
func TestDiffSizeChanged(t *testing.T) {
	local := &Manifest{
		Version: "2.0.0",
		Components: []Component{
			comp("backend.dll", "aaa", 999), // розмір не співпадає
			comp("updater.dll", "bbb", 200),
			comp("frontend.zip", "ccc", 300),
		},
	}
	got := Diff(local, fullRemote())
	if len(got) != 1 || got[0].Path != "backend.dll" {
		t.Fatalf("очікувано backend.dll (змінений розмір), отримано: %+v", got)
	}
}

// TestDiffShaCaseInsensitive — sha256 порівнюється без урахування регістру
// (build.ps1 пише нижнім регістром; захист від різних форматів).
func TestDiffShaCaseInsensitive(t *testing.T) {
	local := &Manifest{
		Version: "2.0.0",
		Components: []Component{
			comp("backend.dll", "AAA", 100),
			comp("updater.dll", "BBB", 200),
			comp("frontend.zip", "CCC", 300),
		},
	}
	if got := Diff(local, fullRemote()); len(got) != 0 {
		t.Fatalf("sha з іншим регістром не має тригернути качку, отримано: %+v", got)
	}
}

// TestCompareVersionsFourSegments — бета-суфікс (4-й сегмент) змушує версію
// бути вищою за стабільну: 2.1.5.1 > 2.1.5, а 2.1.5.0 рівнозначне 2.1.5
// (точковий 0 у кінці не робить версію бета). Саме на цьому рішенні
// побудований вибір каналу у fetchBestRemoteManifest (core.go) — раніше
// CompareVersions порівнював ЛИШЕ 3 сегменти, і 2.1.5.1 == 2.1.5 → бета
// ніколи не вигравала у стабільної.
func TestCompareVersionsFourSegments(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.5.1", "2.1.5", 1},  // бета вища за стабільну
		{"2.1.5", "2.1.5.1", -1}, // і навпаки
		{"2.1.5.0", "2.1.5", 0},  // точковий 0 = та сама версія
		{"2.1.5", "2.1.5.0", 0},
		{"2.1.5.1", "2.1.5.0", 1},
		{"2.1.5.10", "2.1.5.9", 1}, // 4-й сегмент порівнюється як число
		{"2.1.5.1", "2.1.5.1", 0},
		{"2.1.5.1", "2.2.0", -1}, // мажор.мінор важливіші за бета-суфікс
		{"2.2.0", "2.1.5.99", 1},
		{"2.1.5.1-beta.2", "2.1.5", 1}, // суфікс pre-release відкидається
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, очікувано %d", c.a, c.b, got, c.want)
		}
	}
}

// TestPlatformSegmentAndLibraryNames — захищає контракт з core.go
// (fetchRemoteManifest, validateRemoteArchitecture): Windows лишається на
// КОРЕНІ /launcher/ (зворотна сумісність з уже задеплоєним CDN), Linux
// іде через /launcher/linux/; UpdaterLibraryName відповідає реальному
// суфіксу -buildmode=c-shared на кожній ОС. runtime.GOOS фіксований на
// час компіляції тесту, тому перевіряємо контракт для ТІЄЇ платформи, на
// якій тест реально запущено — а не одне захардкожене значення.
func TestPlatformSegmentAndLibraryNames(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		if PlatformSegment() != "" {
			t.Errorf("PlatformSegment() на windows = %q, очікувано порожній рядок (зворотна сумісність)", PlatformSegment())
		}
		if UpdaterLibraryName() != "updater.dll" {
			t.Errorf("UpdaterLibraryName() на windows = %q, очікувано updater.dll", UpdaterLibraryName())
		}
	default:
		if PlatformSegment() != runtime.GOOS+"/" {
			t.Errorf("PlatformSegment() на %s = %q, очікувано %q", runtime.GOOS, PlatformSegment(), runtime.GOOS+"/")
		}
		if UpdaterLibraryName() != "updater.so" {
			t.Errorf("UpdaterLibraryName() на %s = %q, очікувано updater.so", runtime.GOOS, UpdaterLibraryName())
		}
	}

	// ManifestURLPath має завжди включати PlatformSegment між "launcher/"
	// і файлом, незалежно від платформи — саме це відрізняє Windows- і
	// Linux-клієнтів, що звертаються до того самого CDNBaseURL.
	want := "launcher/" + PlatformSegment() + "launcher-version.json"
	if got := ManifestURLPath("launcher-version.json"); got != want {
		t.Errorf("ManifestURLPath() = %q, очікувано %q", got, want)
	}
}

// TestIsLockedComponentMatchesUpdaterLibraryName — IsLockedComponent мав
// баг до підтримки Linux: жорстко звіряв base з константою "updater.dll",
// тому на Linux (де файл called updater.so) НІКОЛИ не спрацьовував —
// апдейтер спробував би замінити власний завантажений .so на місці,
// замість безпечного шляху через .new. Тест захищає від регресу.
func TestIsLockedComponentMatchesUpdaterLibraryName(t *testing.T) {
	path := "some/dir/" + UpdaterLibraryName()
	if !IsLockedComponent(path) {
		t.Errorf("IsLockedComponent(%q) = false, очікувано true (файл апдейтера для поточної ОС)", path)
	}
}

// TestValidateRejectsUnsafe — маніфест із шляхом за межі installDir, чужою
// адресою або без нормального sha256 має бути відхилений ще до завантаження.
func TestValidateRejectsUnsafe(t *testing.T) {
	good := Component{
		Path: "backend.dll", URL: "/launcher/backend.dll", Size: 10,
		SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	if err := (&Manifest{Components: []Component{good}}).Validate(); err != nil {
		t.Fatalf("коректний компонент відхилено: %v", err)
	}
	bad := map[string]func(c *Component){
		"..-у шляху":        func(c *Component) { c.Path = "../evil.dll" },
		"абсолютний шлях":   func(c *Component) { c.Path = "/etc/passwd" },
		"диск у шляху":      func(c *Component) { c.Path = `C:\Windows\x.dll` },
		"зворотний слеш ..": func(c *Component) { c.Path = `a\..\..\x` },
		"userinfo-url":      func(c *Component) { c.URL = "@evil.example/x" },
		"url без слеша":     func(c *Component) { c.URL = "launcher/backend.dll" },
		"protocol-relative": func(c *Component) { c.URL = "//evil.example/x" },
		"..-в url":          func(c *Component) { c.URL = "/launcher/../../x" },
		"порожній sha":      func(c *Component) { c.SHA256 = "" },
		"не hex sha":        func(c *Component) { c.SHA256 = "zz" + good.SHA256[2:] },
		"нульовий розмір":   func(c *Component) { c.Size = 0 },
		"extractTo за межі": func(c *Component) { c.Kind = "archive"; c.ExtractTo = "../up" },
	}
	for name, mut := range bad {
		c := good
		mut(&c)
		if err := (&Manifest{Components: []Component{c}}).Validate(); err == nil {
			t.Errorf("%s: небезпечний компонент не відхилено: %+v", name, c)
		}
	}
}
