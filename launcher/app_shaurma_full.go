//go:build shaurma

package main

import (
	"fmt"
	"path/filepath"

	"shaurma-launcher-wails/internal/api"
	"shaurma-launcher-wails/internal/sync"
)

func newPackProvider(a *App) PackProvider {
	return &shaurmaProvider{
		api: api.NewShaurmaClient(),
		app: a,
	}
}

// IsShaurmaEdition повертає true тільки у повній (shaurma) збірці.
// Фронтенд питає цей прапорець, щоб показувати вкладку/список збірок
// Шаурма лише там, де відповідні бекенд-методи реально скомпільовані.
func (a *App) IsShaurmaEdition() bool { return true }

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

// DownloadPack (V2, DOWNLOAD_SYNC_DESIGN_V2.md) якає ОДИН легкий
// packs/<id>/manifest.json (замість цілого .mrpack), рахує дельту проти
// локального InstallState і синхронізує через двочерговий sync.Runner.
// syncMgr.Start сам відхиляє повторний виклик для вже активної збірки —
// тому кілька РІЗНИХ збірок можна якати одночасно без взаємних гонок, а
// одна й та сама збірка не запуститься вдруге поверх себе.
func (p *shaurmaProvider) DownloadPack(packID string, a *App) {
	settings := a.cfg.GetSettings()
	// Тека гри збірки — installDir/.minecraft, ЯК У СТАРОГО ЛАУНЧЕРА:
	// старий (Java) розпаковував overrides у installations/<id>/.minecraft/
	// і запускав гру з --gameDir <packDir>/.minecraft. Новий синк раніше
	// писав моди/конфіги прямо в корінь installations/<packID>/, через що
	// гра не бачила своїх файлів.
	instDir := filepath.Join(settings.InstanceDir, packID, ".minecraft")

	go func() {
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

		runner := sync.NewRunner(
			packID, packName, instDir,
			p.api.BaseURL(), p.api.Token(),
			p.api.HTTPClientForSync(),
			a.dl, // download.Engine реалізує sync.Submitter (спільний адаптивний пул)
			state, stateStore,
			a.cfg.GetSettings().MaxRetries,
			nil, // onProgress — підвішується всередині syncMgr.Start
		)

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

		switch runner.Mode() {
		case sync.ModeComplete:
			a.emit("download:complete", packID)
			a.emit("installer:log", "Завантаження завершено!")
			go func() {
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
