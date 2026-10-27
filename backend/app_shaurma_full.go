//go:build shaurma

package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"fmt"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/sync"
)

func newPackProvider(a *App) PackProvider {
	client := api.NewShaurmaClient()
	// Ідентичність для приватних збірок. Microsoft/Minecraft/Google-токени на сервер НЕ
	// передаються. Підтверджену особу дає лише комірка акаунта Шаурми: VPS видає короткий
	// підписаний тікет (його перевіряє Worker локально, лише за підписом). Акаунти без комірки
	// (лише нік) йдуть ніком — він непідтверджений і ніколи не задовольнить licensedOnly.
	// Провайдер викликається на кожен запит — створення/видалення акаунта підхоплюється
	// без перезапуску.
	client.SetAccountsProvider(func() []sync.AccountCred {
		var creds []sync.AccountCred
		for _, acc := range a.cfg.GetAccounts() {
			if _, ok := a.shaurmaCell(acc.ID); ok {
				if t := a.shaurmaACLTicket(acc.ID); t != "" {
					creds = append(creds, sync.AccountCred{Token: t})
					continue
				}
			}
			if acc.IsLicensed {
				continue // ліцензійний без комірки доступу не дає
			}
			if acc.Username != "" {
				creds = append(creds, sync.AccountCred{Nick: acc.Username})
			}
		}
		return creds
	})
	return &shaurmaProvider{
		api: client,
		app: a,
	}
}

// IsShaurmaEdition повертає true тільки у повній (shaurma) збірці.
// Фронтенд питає цей прапорець, щоб показувати вкладку/список збірок
// Шаурма лише там, де відповідні бекенд-методи реально скомпільовані.
func (a *App) IsShaurmaEdition() bool { return true }

// launcherEditionName — назва редакції для аналітики та install-ping.
func launcherEditionName() string { return "shaurma" }

type shaurmaProvider struct {
	api *api.ShaurmaClient
	app *App
}

func (p *shaurmaProvider) GetPackIndex() ([]api.PackIndexEntry, error) {
	return p.api.GetPackIndex()
}

func (p *shaurmaProvider) GetPackByID(id string) (*api.PackIndexEntry, error) {
	return p.api.GetPackByID(id)
}

// AssetDataURL — data URL іконки/фону збірки (див. ShaurmaClient.AssetDataURL):
// качає асет з CDN з токеном у заголовках і повертає як data: URL для <img>.
func (p *shaurmaProvider) AssetDataURL(assetURL string) (string, error) {
	return p.api.AssetDataURL(assetURL)
}

// GetPackWhitelist — повні записи білого списку (глобальний + per-pack)
// для картки мода в UI (див. ShaurmaClient.GetEffectiveWhitelistDetailed).
func (p *shaurmaProvider) GetPackWhitelist(packID string) ([]sync.WhitelistEntry, error) {
	return p.api.GetEffectiveWhitelistDetailed(packID)
}

// GetPackWiki — режим вікі збірки (packs/<id>/wiki.json). nil, nil коли
// адмін ще не створив вікі (404).
func (p *shaurmaProvider) GetPackWiki(packID string) (*sync.PackWikiDoc, error) {
	return p.api.GetPackWiki(packID)
}

// InvalidatePackCache — див. PackProvider.InvalidatePackCache. Викликається
// з AccountsChanged (backend/accounts.go) одразу після входу/виходу/
// перемикання акаунта.
func (p *shaurmaProvider) InvalidatePackCache() {
	p.api.InvalidatePackIndexCache()
}

// DownloadPack (V2, DOWNLOAD_SYNC_DESIGN_V2.md) якає ОДИН легкий
// packs/<id>/manifest.json (замість цілого .mrpack), рахує дельту проти
// локального InstallState і синхронізує через двочерговий sync.Runner.
// syncMgr.Start сам відхиляє повторний виклик для вже активної збірки —
// тому кілька РІЗНИХ збірок можна якати одночасно без взаємних гонок, а
// одна й та сама збірка не запуститься вдруге поверх себе.
func (p *shaurmaProvider) DownloadPack(packID string, a *App) {
	// Тека гри збірки — installDir/.minecraft, ЯК У СТАРОГО ЛАУНЧЕРА:
	// старий (Java) розпаковував overrides у installations/<id>/.minecraft/
	// і запускав гру з --gameDir <packDir>/.minecraft. Новий синк раніше
	// писав моди/конфіги прямо в корінь installations/<packID>/, через що
	// гра не бачила своїх файлів.
	instDir := a.gameDir(packID)

	go func() {
		defer crashguard.Recover("app_shaurma_full.go:103")
		a.emit("installer:log", "Завантаження маніфесту збірки...")

		// Health-check ПЕРЕД манфестом: якщо мережі взагалі немає, кажемо
		// про це прямо, а не показуємо загальну помилку "не вдалось
		// завантажити маніфест" — саме те розрізнення, яке раніше плутало
		// користувача ("здається зависло").
		if err := p.api.CheckWorkerHealth(); err != nil {
			// Мітимо невдалий старт: waitForSyncStartThenFinish (запуск збірки)
			// побачить це і не зависне на 20с, чекаючи runner, який не з'явиться.
			a.markSyncFailure(packID)
			if sync.IsNoInternet(err) {
				a.emit("sync:no-internet", packID)
			} else {
				a.emit("download:error", fmt.Sprintf("Сервер завантажень тимчасово недоступний: %s", err.Error()))
			}
			return
		}

		manifest, err := p.api.GetPackManifest(packID)
		if err != nil {
			a.markSyncFailure(packID)
			if sync.IsNoInternet(err) {
				a.emit("sync:no-internet", packID)
			} else {
				a.emit("download:error", err.Error())
			}
			return
		}

		entry, _ := p.api.GetPackByID(packID)
		packName := packID
		if entry != nil {
			packName = entry.Name
		}

		a.emit("installer:log", fmt.Sprintf(
			"Збірка: %s | Minecraft %s | Моди(хеш): %d | Файли(worker): %d",
			manifest.ID, manifest.MCVersion, len(manifest.HashMods), len(manifest.WorkerFiles)))

		stateStore := a.syncStateStore()
		state := stateStore.Load(packID)
		// isFirstInstall фіксуємо ДО старту синку (Runner мутує state по
		// ходу роботи): StageNone/StageComplete-порожній стан означає, що
		// збірки тут ще не було. Це розрізнення потрібне нижче — дефолтні
		// налаштування гри (options.txt) мають писатись ОДРАЗУ лише при
		// першому встановленні, а не при кожному звичайному оновленні
		// (див. коментар біля applyMinecraftDefaultsNow нижче).
		isFirstInstall := state.Stage == sync.StageNone

		runner := sync.NewRunner(
			packID, packName, instDir,
			p.api.BaseURL(), p.api.Token(),
			p.api.HTTPClientForSync(),
			a.dl, // download.Engine реалізує sync.Submitter (спільний адаптивний пул)
			state, stateStore,
			a.cfg.GetSettings().MaxRetries,
			nil, // onProgress — підвішується всередині syncMgr.Start
		)

		// Білий список модів: жара, чий mod-id (НЕ ім'я файлу — той самий
		// мод різних версій має різний jar) потрапив у глобальний або
		// per-pack білий список, синк НЕ видаляє, навіть якщо його немає в
		// манфесті. Мережева помилка тут не провалює завантаження збірки —
		// просто діємо так, ніби білий список порожній (найбезпечніший
		// дефолт: не видаляти зайве через тимчасовий збій мережі теж не
		// можна, тому whitelist == nil → IsWhitelisted лишається nil →
		// buildPlanWithModsAndWhitelist поверне СТАРУ поведінку строгого
		// видалення без винятків, а не "дозволити все").
		if whitelist, err := p.api.GetEffectiveWhitelist(packID); err == nil && len(whitelist) > 0 {
			modsDir := filepath.Join(instDir, "mods")
			runner.IsWhitelisted = func(fileName string) bool {
				id := a.modMgr.ModIdentityOf(filepath.Join(modsDir, fileName))
				if id.ID != "" && whitelist[strings.ToLower(id.ID)] {
					return true
				}
				for _, provides := range id.Provides {
					if whitelist[strings.ToLower(provides)] {
						return true
					}
				}
				return false
			}
		}

		maxConcurrent := a.cfg.GetSettings().MaxConcurrentDownloads
		// Resume після Pause: paused-runner може ще дренажитись (докачує
		// поточний чанк), і Start для тієї самої збірки одразу відхилив би
		// повторний запуск помилкою «вже синхронізується» — користувач
		// побачив би зайвий тост помилки. Дочекаємось завершення старої
		// сесії, потім стартуємо нову (вона підхопить .part-прогрес).
		a.syncMgr.WaitFor(packID)
		done, err := a.syncMgr.Start(runner, manifest, maxConcurrent)
		if err != nil {
			a.emit("download:error", err.Error())
			return
		}
		<-done // ЦЯ горутина просто чекає завершення; прогрес іде через syncMgr.onEvent

		// Короткий звіт "що саме змінилось на диску" у консоль
		// встановлення — повний слід і так пишеться в logs/launcher.log
		// (компонент "Sync") по ходу синку, а тут даємо гравцю зведення
		// одразу в UI, без пошуку по файлу логів. Друкуємо навіть при
		// Pause/помилці (не тільки ModeComplete) — часткова синхронізація
		// теж могла щось змінити на диску.
		a.emitSyncReport(runner.Report())

		switch runner.Mode() {
		case sync.ModeComplete:
			// Дефолтні налаштування гри (options.txt) пишемо ОДРАЗУ лише
			// при першому встановленні збірки — applyMinecraftDefaultsNow
			// сам звіряється з instance.json.MinecraftDefaultsApplied і
			// нічого не робить, якщо дефолти вже застосовувались раніше.
			// Для звичайного оновлення вже встановленої збірки виклик тут
			// НЕ потрібен: options.txt/config гравця (правило «Оновити
			// при старті») не має затиратись при кожному оновленні.
			// ApplyBeforeLaunch (перед стартом гри) сама так само
			// звіряється з instance.json і застосує дефолти рівно один
			// раз за все життя збірки, якщо це ще не сталось тут.
			if isFirstInstall {
				a.applyMinecraftDefaultsNow(packID)
			}
			// Офіційний сервер збірки (serverIp з маніфесту).
			//   - Звичайний режим: лише при першому встановленні servers.dat ще
			//     не створений — записуємо офіційний сервер, щоб він одразу був у
			//     Multiplayer (якщо гравець спорожнить список — поверне
			//     ensureOfficialServer у LaunchInstance).
			//   - officialServerSync: після КОЖНОГО успішного оновлення збірки
			//     ставимо/оновлюємо сервер з актуальним IP із CDN.
			if isFirstInstall || manifest.OfficialServerSync {
				a.ensureOfficialServer(packID, packName, manifest.ServerIP, manifest.OfficialServerSync)
			}
			a.emit("download:complete", packID)
			a.emit("installer:log", "Завантаження завершено!")
			go func() {
				defer crashguard.Recover("app_shaurma_full.go:233")
				a.emit("installer:log", fmt.Sprintf("Встановлення Minecraft %s...", manifest.MCVersion))
				if _, err := a.inst.EnsureVersion(manifest.MCVersion); err != nil {
					a.emit("installer:log", "Помилка: "+err.Error())
					return
				}
				a.emit("installer:log", "Minecraft встановлено!")
			}()
		case sync.ModePaused:
			a.emit("installer:log", "Завантаження зупинено — можна продовжити пізніше.")
		case sync.ModeCancelled:
			a.emit("installer:log", "Завантаження скасовано.")
		case sync.ModeError:
			a.emit("download:error", "Синхронізацію перервано через помилку")
		}
	}()
}

// PauseDownload — м'яка зупинка (прогрес зберігається, кнопка "Продовжити").
func (p *shaurmaProvider) PauseDownload(packID string) {
	p.app.syncMgr.Pause(packID)
}

// CancelDownload — повне скасування (часткові файли видаляються).
func (p *shaurmaProvider) CancelDownload(packID string) {
	p.app.syncMgr.Cancel(packID)
}
