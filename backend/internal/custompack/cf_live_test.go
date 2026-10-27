package custompack

// ── Живі інтеграційні тести проти РЕАЛЬНОГО CurseForge API ───────────────
// Перевіряють те, що юніт-тести на локальному сервері не можуть покрити:
//   - «шукаємо версії мода/ресурса під (mcVersion, loader) збірки» — кожен
//     повернутий файл має реально містити і версію гри, і лоадер;
//   - порядок «новіші перші» (versions[0] = найсвіжіший);
//   - канал релізу (releaseType): коли під збірку є і стабільний реліз, і
//     новіші beta/alpha, «остання сумісна версія» — це реліз, а не beta;
//   - «фільтруємо збірки згідно мода»: для лоадера, якого мод не має,
//     файлів немає (жодного «несумісного» файлу у видачі).
//
// Ці тести живуть ОКРЕМО від офлайн-тестів: без мережі вони пропускаються.
// Ключ CurseForge задається env CF_API_KEY (той самий, що build.bat вшиває
// у backend через -ldflags):
//
//	CF_API_KEY='$2a$10$...' go test ./internal/custompack/ -run TestCFLive -v
//
// Очікування зафіксовані на реальних проєктах CurseForge (перевірено 2026-09):
//   - JEI            (238222): forge+fabric, 186 файлів під 1.20.1/forge (пагінація);
//     для 1.19.2/1.21.1 найсвіжіші файли — beta, стабільний release нижче;
//   - Sodium         (394468): лише fabric (forge-фільтр = 0 файлів);
//   - Fabric API     (306612): лише fabric (forge-фільтр = 0 файлів);
//   - Create         (328085): forge;
//   - Quark          (243121): forge;
//   - AppleSkin      (248787): fabric, 1.20.1 — ЛИШЕ beta/alpha (без release).

import (
	"os"
	"strings"
	"testing"
)

// cfLiveKey або пропускає тест, якщо ключа немає (CI/офлайн).
func cfLiveKey(t *testing.T) string {
	t.Helper()
	k := os.Getenv("CF_API_KEY")
	if strings.TrimSpace(k) == "" {
		t.Skip("CF_API_KEY не задано — живий CurseForge-тест пропущено")
	}
	SetUserCurseForgeAPIKey(k)
	t.Cleanup(func() { SetUserCurseForgeAPIKey("") })
	return k
}

// cfLiveCase — один рядок матриці «проєкт × (mc, loader) → очікування».
type cfLiveCase struct {
	name      string
	projectID int
	mc        string
	loader    string // "" = без фільтра лоадера (ресурспаки/датапаки)
	// wantEmpty — для НЕсумісного лоадера файлів не має бути зовсім.
	wantEmpty bool
	// wantMin — мінімум файлів під (mc, loader) (0 = не перевіряємо).
	wantMin int
	// wantRelease — під цією парою має бути ХОЧ ОДИН стабільний release
	// (і авто-вибір «останньої сумісної» має впасти саме на нього).
	wantRelease bool
	// mustNotLoaders — лоадери, яких НЕ має бути в жодному файлі видачі
	// (перевірка, що серверний + локальний фільтр не пускають «чужих»).
	mustNotLoaders []string
}

func cfLiveMatrix() []cfLiveCase {
	return []cfLiveCase{
		// JEI forge 1.20.1 — 186 файлів: доводить багатосторінкову вибірку
		// (GetModFiles ходить сторінками по 50, а не ріже перші 50).
		{name: "JEI forge 1.20.1 (multi-page)", projectID: 238222, mc: "1.20.1", loader: "forge",
			wantMin: 150, wantRelease: true, mustNotLoaders: []string{"fabric", "quilt"}},
		// JEI forge 1.19.2 / 1.21.1 — найсвіжіші файли beta, release нижче.
		{name: "JEI forge 1.19.2 (beta above release)", projectID: 238222, mc: "1.19.2", loader: "forge",
			wantMin: 20, wantRelease: true, mustNotLoaders: []string{"fabric", "quilt"}},
		{name: "JEI forge 1.21.1 (beta above release)", projectID: 238222, mc: "1.21.1", loader: "forge",
			wantMin: 20, wantRelease: true, mustNotLoaders: []string{"fabric", "quilt"}},
		{name: "JEI fabric 1.20.1", projectID: 238222, mc: "1.20.1", loader: "fabric",
			wantMin: 20, wantRelease: true, mustNotLoaders: []string{"forge"}},
		{name: "Sodium fabric 1.20.1", projectID: 394468, mc: "1.20.1", loader: "fabric",
			wantMin: 5, wantRelease: true, mustNotLoaders: []string{"forge"}},
		{name: "Fabric API fabric 1.20.1", projectID: 306612, mc: "1.20.1", loader: "fabric",
			wantMin: 5, wantRelease: true, mustNotLoaders: []string{"forge"}},
		{name: "Create forge 1.20.1", projectID: 328085, mc: "1.20.1", loader: "forge",
			wantMin: 5, wantRelease: true, mustNotLoaders: []string{"fabric"}},
		{name: "Quark forge 1.20.1", projectID: 243121, mc: "1.20.1", loader: "forge",
			wantMin: 5, wantRelease: true, mustNotLoaders: []string{"fabric"}},
		// AppleSkin fabric 1.20.1 — ВСІ файли beta/alpha: авто-вибір має
		// коректно впасти на найсвіжіший (release немає взагалі).
		{name: "AppleSkin fabric 1.20.1 (only beta/alpha)", projectID: 248787, mc: "1.20.1", loader: "fabric",
			wantMin: 1, wantRelease: false, mustNotLoaders: []string{"forge"}},
		// Несумісні лоадери: файлів не має бути зовсім.
		{name: "Sodium forge 1.20.1 (incompatible loader)", projectID: 394468, mc: "1.20.1", loader: "forge", wantEmpty: true},
		{name: "Fabric API forge 1.20.1 (incompatible loader)", projectID: 306612, mc: "1.20.1", loader: "forge", wantEmpty: true},
	}
}

// TestCFLiveFilesMatrix — «версії мода під (mc, loader) збірки» на живих
// проєктах: кожен повернутий файл має містити і версію гри, і потрібний
// лоадер (без чужих лоадерів), кількість файлів відповідає очікуваній, а
// список відсортований «новіші перші».
func TestCFLiveFilesMatrix(t *testing.T) {
	cfLiveKey(t)
	c := NewCurseForgeClient()

	for _, tc := range cfLiveMatrix() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			files, err := c.GetModFiles(tc.projectID, tc.mc, tc.loader)
			if err != nil {
				t.Fatalf("GetModFiles(%d, %q, %q): %v", tc.projectID, tc.mc, tc.loader, err)
			}
			if tc.wantEmpty {
				if len(files) != 0 {
					t.Fatalf("для НЕсумісного лоадера отримали %d файлів (перший: %s) — фільтр по збірці пропустив чужий лоадер", len(files), files[0].FileName)
				}
				return
			}
			if tc.wantMin > 0 && len(files) < tc.wantMin {
				t.Fatalf("файлів %d, хотіли >= %d", len(files), tc.wantMin)
			}
			for i, f := range files {
				if !containsStr(f.GameVersions, tc.mc) {
					t.Fatalf("файл %d %q не містить версії %q (gameVersions=%v)", i, f.FileName, tc.mc, f.GameVersions)
				}
				if tc.loader != "" && !containsLoader(f.GameVersions, tc.loader) {
					t.Fatalf("файл %d %q не містить лоадера %q (gameVersions=%v)", i, f.FileName, tc.loader, f.GameVersions)
				}
				for _, bad := range tc.mustNotLoaders {
					if containsLoader(f.GameVersions, bad) {
						t.Fatalf("файл %d %q містить ЧУЖИЙ лоадер %q (gameVersions=%v)", i, f.FileName, bad, f.GameVersions)
					}
				}
			}
			// Порядок «новіші перші» по всьому списку.
			for i := 1; i < len(files); i++ {
				if cfParseFileDate(files[i].FileDate).After(cfParseFileDate(files[i-1].FileDate)) {
					t.Fatalf("порядок порушено: файл %d (%s, %s) новіший за %d (%s, %s)",
						i, files[i].FileName, files[i].FileDate, i-1, files[i-1].FileName, files[i-1].FileDate)
				}
			}
		})
	}
}

// TestCFLiveRecommendedPrefersStableRelease — «остання сумісна версія під
// збірку» має бути останнім СТАБІЛЬНИМ release, а не найсвіжішою beta:
// реальні JEI 1.19.2/1.21.1 (свіжі файли — beta, release нижче за датою).
// Той самий вибір, що робить pickBestBrowserVersion через
// CFReleaseTypeName: перший файл каналу "release", інакше "beta", інакше
// "alpha", інакше найсвіжіший.
func TestCFLiveRecommendedPrefersStableRelease(t *testing.T) {
	cfLiveKey(t)
	c := NewCurseForgeClient()

	for _, tc := range cfLiveMatrix() {
		if tc.wantEmpty || !tc.wantRelease {
			continue
		}
		tc := tc
		t.Run("recommended_"+tc.name, func(t *testing.T) {
			files, err := c.GetModFiles(tc.projectID, tc.mc, tc.loader)
			if err != nil {
				t.Fatalf("GetModFiles: %v", err)
			}
			if len(files) == 0 {
				t.Fatalf("порожній список — тест безглуздий")
			}
			// Наївний вибір (як БУЛО ДО фіксу): найсвіжіший файл versions[0].
			naive := files[0]
			// Правило release → beta → alpha.
			chosen := pickLatestStable(files)
			if chosen == nil {
				t.Fatalf("не вдалось обрати файл")
			}
			if CFReleaseTypeName(naive.ReleaseType) == "beta" && CFReleaseTypeName(chosen.ReleaseType) != "release" {
				t.Fatalf("найсвіжіший файл — beta, але вибір не впав на release: naive=%s(%s) chosen=%s(%s)",
					naive.FileName, CFReleaseTypeName(naive.ReleaseType), chosen.FileName, CFReleaseTypeName(chosen.ReleaseType))
			}
			if CFReleaseTypeName(chosen.ReleaseType) != "release" {
				t.Fatalf("серед файлів є release, але обрано не його: %s (%s)",
					chosen.FileName, CFReleaseTypeName(chosen.ReleaseType))
			}
		})
	}
}

// pickLatestStable — вибір «найкращої версії» з уже відсортованого
// (новіші-перші) списку: перший release, інакше перша beta, інакше перша
// alpha, інакше найсвіжіший файл. Дзеркало pickBestBrowserVersion
// (backend/browser_api.go), яке працює через CFReleaseTypeName.
func pickLatestStable(files []CFFileHit) *CFFileHit {
	var beta, alpha *CFFileHit
	for i := range files {
		switch files[i].ReleaseType {
		case 1:
			return &files[i]
		case 2:
			if beta == nil {
				beta = &files[i]
			}
		case 3:
			if alpha == nil {
				alpha = &files[i]
			}
		}
	}
	if beta != nil {
		return beta
	}
	if alpha != nil {
		return alpha
	}
	if len(files) > 0 {
		return &files[0]
	}
	return nil
}
