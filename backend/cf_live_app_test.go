package main

// ── Живі App-тести проти РЕАЛЬНОГО CurseForge API ───────────────────────
// Покривають саме сценарій «фільтруємо/сортуємо за збіркою» на рівні App:
//   - GetBrowserCompatiblePacks: які збірки (mcVersion+loader) сумісні з
//     цим модом CurseForge — forge-мод НЕ пропонується у fabric-збірку і
//     навпаки, а сумісні версії збірок проходять;
//   - GetBrowserRecommendedVersion / pickBestBrowserVersion: «остання
//     сумісна версія» для збірки — останній СТАБІЛЬНИЙ release, а не
//     найсвіжіша beta (реально: JEI 1.19.2/1.21.1 — beta вище release).
//
// Пропускаються без env CF_API_KEY (той самий ключ, що в build.bat).
//
//	CF_API_KEY='$2a$10$...' go test . -run TestAppCFLive -v

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/sync"
)

// liveCFPacks — PackProvider без шаурма-збірок (порожній індекс): білий
// список ніколи не спрацьовує, мережевих запитів до CDN Шаурми немає.
type liveCFPacks struct{}

func (liveCFPacks) GetPackIndex() ([]api.PackIndexEntry, error) { return nil, nil }
func (liveCFPacks) GetPackByID(id string) (*api.PackIndexEntry, error) {
	return nil, fmt.Errorf("pack %s not found", id)
}
func (liveCFPacks) DownloadPack(packID string, a *App)           {}
func (liveCFPacks) PauseDownload(packID string)                  {}
func (liveCFPacks) CancelDownload(packID string)                 {}
func (liveCFPacks) AssetDataURL(assetURL string) (string, error) { return "", nil }
func (liveCFPacks) GetPackWhitelist(packID string) ([]sync.WhitelistEntry, error) {
	return nil, nil
}

// GetPackWiki — у цьому стабі немає шаурма-каталогу, тож вікі теж немає:
// nil, nil = «вікі не створено», як і 404 з CDN.
func (liveCFPacks) GetPackWiki(packID string) (*sync.PackWikiDoc, error) {
	return nil, nil
}

// InvalidatePackCache — частина PackProvider; у стабі кешу немає, тож no-op.
// Без цього методу `go vet` (а отже й `go test`) падав на компіляції
// інтерфейсу: liveCFPacks does not implement PackProvider.
func (liveCFPacks) InvalidatePackCache() {}

// appLiveCFKey пропускає тест без ключа і ставить власний ключ користувача.
func appLiveCFKey(t *testing.T) string {
	t.Helper()
	k := os.Getenv("CF_API_KEY")
	if strings.TrimSpace(k) == "" {
		t.Skip("CF_API_KEY не задано — живий CurseForge-тест пропущено")
	}
	custompack.SetUserCurseForgeAPIKey(k)
	t.Cleanup(func() { custompack.SetUserCurseForgeAPIKey("") })
	return k
}

// newAppLiveCF будує App з чотирма кастомними збірками різних
// (mcVersion, loader) — кандидатами для «куди можна встановити цей мод».
func newAppLiveCF(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a, cfg := newTestApp(t, dir, "fab120", "1.20.1", struct{ Loader, Version string }{"fabric", "x"})
	a.packs = liveCFPacks{}
	store := custompack.NewStore(cfg.ConfigDir())
	for _, p := range []struct{ id, mc, loader string }{
		{"fab120", "1.20.1", "fabric"},
		{"forge120", "1.20.1", "forge"},
		{"forge192", "1.19.2", "forge"},
		{"forge1211", "1.21.1", "forge"},
	} {
		if err := store.Save(custompack.Pack{
			ID: p.id, Name: p.id, MCVersion: p.mc, Loader: p.loader, LoaderVersion: "x",
		}); err != nil {
			t.Fatalf("save pack %s: %v", p.id, err)
		}
	}
	a.customPacks = store
	return a
}

func cfPackIDs(packs []BrowserCompatiblePack) []string {
	out := make([]string, 0, len(packs))
	for _, p := range packs {
		if !p.Unconfirmed && !p.Whitelisted {
			out = append(out, p.PackID)
		}
	}
	sort.Strings(out)
	return out
}

// TestAppCFLiveCompatiblePacksFilter — «фільтруємо збірки згідно мода»:
//   - Sodium (лише fabric) — у forge-збірки НЕ пропонується;
//   - Fabric API (лише fabric) — те саме;
//   - JEI (forge 1.19.2/1.20.1/1.21.1 + fabric 1.20.1) — пропонується у
//     всі сумісні, у несумісні (немає) — ні.
//
// Очікування звірені з живим API (2026-09): див. cf_live_test.go.
func TestAppCFLiveCompatiblePacksFilter(t *testing.T) {
	appLiveCFKey(t)
	a := newAppLiveCF(t)

	tests := []struct {
		name      string
		projectID string
		want      []string
	}{
		{"Sodium (fabric-only)", "394468", []string{"fab120"}},
		{"Fabric API (fabric-only)", "306612", []string{"fab120"}},
		{"JEI (forge+fabric)", "238222", []string{"fab120", "forge120", "forge192", "forge1211"}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			packs := a.GetBrowserCompatiblePacks("curseforge", tc.projectID, "mod", "mods", "")
			got := cfPackIDs(packs)
			want := tc.want
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("сумісні збірки = %v, хотіли %v (всі: %+v)", got, want, packs)
			}
		})
	}
}

// TestAppCFLiveRecommendedStableNotBeta — «остання сумісна версія під
// збірку (mc, loader)» для CurseForge має бути останнім СТАБІЛЬНИМ
// release. Реально JEI 1.19.2/1.21.1 тримають найсвіжіші beta-файли ВИЩЕ
// за датою, ніж останній release; до фіксу pickBestBrowserVersion бачив
// порожній VersionType (CF не заповнювався) і брав versions[0] = beta.
func TestAppCFLiveRecommendedStableNotBeta(t *testing.T) {
	appLiveCFKey(t)
	a := newAppLiveCF(t)

	for _, mc := range []string{"1.19.2", "1.21.1"} {
		mc := mc
		t.Run("JEI forge "+mc, func(t *testing.T) {
			versions, err := a.GetBrowserProjectVersions("curseforge", "238222", "mod", mc, "forge")
			if err != nil {
				t.Fatalf("GetBrowserProjectVersions: %v", err)
			}
			if len(versions) == 0 {
				t.Fatalf("порожній список версій для JEI %s/forge", mc)
			}
			firstRelease := -1
			for i := range versions {
				if versions[i].VersionType == "release" {
					firstRelease = i
					break
				}
			}
			// Сценарій має сенс лише коли release взагалі є.
			if firstRelease == -1 {
				t.Skipf("release для JEI %s/forge не знайдено (можливо, змінилось на CurseForge)", mc)
			}
			recommended, err := a.GetBrowserRecommendedVersion("curseforge", "238222", "mod", mc, "forge")
			if err != nil {
				t.Fatalf("GetBrowserRecommendedVersion: %v", err)
			}
			if recommended == nil {
				t.Fatalf("немає рекомендованої версії для JEI %s/forge", mc)
			}
			if recommended.VersionType != "release" {
				t.Fatalf("рекомендована версія = %s (%s), хотіли останній release; список: перший=%s(%s), перший release на idx %d (%s)",
					recommended.VersionNumber, recommended.VersionType,
					versions[0].VersionNumber, versions[0].VersionType, firstRelease, versions[firstRelease].VersionNumber)
			}
			if versions[firstRelease].ID != recommended.ID {
				t.Fatalf("рекомендована (%s, idx?) не збігається з першим release у списку (%s, idx %d)",
					recommended.VersionNumber, versions[firstRelease].VersionNumber, firstRelease)
			}
		})
	}
}
