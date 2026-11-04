package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shrm-updater-backend/internal/authenticode"
	"shrm-updater-backend/internal/download"
	"shrm-updater-backend/internal/install"
	"shrm-updater-backend/internal/manifest"
	"shrm-updater-backend/internal/newupdate"
)

// updateSource — звідки качати вибране оновлення.
type updateSource struct {
	remote        *manifest.Manifest
	baseURL       string
	token         string
	allowRedirect func(string) bool
	plan          *newupdate.Plan // nil — старий CDN (міст R1.x)
}

// currentShellName — ім'я exe, у якому зараз працює оновлювач (updater.dll
// живе в процесі shell). Старі Base-установки мають ShaurmLauncherBase.exe,
// і єдиний лаунчер ставиться саме під цим ім'ям — без перейменувань і ярликів.
func currentShellName(edition Channel) string {
	if exe, err := os.Executable(); err == nil {
		base := filepath.Base(exe)
		for _, ch := range []Channel{ChannelShaurma, ChannelBase} {
			if strings.EqualFold(base, launcherExeName(ch)) {
				return base
			}
		}
	}
	return launcherExeName(edition)
}

// runNew — нова система оновлень (§5А) з мостом на старий CDN, поки у збірці
// є токен каналу (R1). Будь-яка невдача перевірки = лаунчер працює на
// поточній версії, без фолбеку на непідписане.
func runNew(ctx context.Context, installDir string, edition Channel, updateChannel string, installUpdates bool, local *manifest.Manifest, token string, start time.Time, cb Callbacks) error {
	shellName := currentShellName(edition)
	launcherInstalled := fileExists(filepath.Join(installDir, shellName))
	launchCurrent := func(msg string) error {
		if !launcherInstalled {
			return errors.New(msg)
		}
		notify(cb, StatusUpToDate, msg)
		holdSplash(start, minSplashShow)
		return nil
	}

	notify(cb, StatusChecking, cb.meme(StatusChecking, 0))
	eng, err := newupdate.Open(newupdate.Options{
		InstallDir:     installDir,
		Channel:        updateChannel,
		Edition:        string(edition),
		ShellName:      shellName,
		CurrentVersion: local.Version,
	})
	if err != nil {
		return launchCurrent(cb.tr("Перевірку оновлень вимкнено: ", "Update check disabled: ") + err.Error())
	}
	defer eng.Close()
	if eng.Notice != "" {
		notify(cb, StatusChecking, eng.Notice)
	}

	var best *updateSource
	plan, planErr := eng.Check(ctx)
	switch {
	case errors.Is(planErr, newupdate.ErrCertGate):
		// Ворота сертифіката закриті: не качаємо НІЧОГО (ні з GitHub, ні з CDN).
		return launchCurrent(cb.tr("Сертифікат ще не збережено — оновлення відкладено", "Certificate not saved yet — update postponed"))
	case errors.Is(planErr, newupdate.ErrClock):
		return launchCurrent(cb.tr("Перевірте дату й час на ПК — оновлення відкладено", "Check the date and time on this PC — update postponed"))
	case planErr == nil && plan != nil:
		best = &updateSource{remote: plan.Remote, baseURL: plan.BaseURL, allowRedirect: plan.AllowRedirect, plan: plan}
	}

	// Міст R1.x: старий CDN (лише з вшитим токеном і ОБОВ'ЯЗКОВИМ підписом).
	if token != "" {
		if legacy, lerr := fetchBestRemoteManifest(ctx, token, updateChannel, cb); lerr == nil {
			if best == nil || manifest.CompareVersions(legacy.Version, best.remote.Version) > 0 {
				best = &updateSource{remote: legacy, baseURL: CDNBaseURL, token: token}
			}
		}
	}

	if best == nil {
		if planErr != nil && !errors.Is(planErr, newupdate.ErrDeferred) {
			return launchCurrent(cb.tr("Оновлення недоступне: ", "Update unavailable: ") + planErr.Error())
		}
		if errors.Is(planErr, newupdate.ErrDeferred) {
			return launchCurrent(cb.tr("Оновлення буде трохи пізніше", "The update will arrive a bit later"))
		}
		if installUpdates && launcherInstalled {
			// Версія та сама, але файлів може бракувати (видалені вручну).
			if missing := missingRequiredComponents(installDir, local); len(missing) > 0 && token != "" {
				best = &updateSource{remote: local, baseURL: CDNBaseURL, token: token}
			}
		}
		if best == nil {
			notify(cb, StatusUpToDate, cb.tr("Лаунчер актуальний", "Launcher is up to date"))
			holdSplash(start, minSplashShow)
			return nil
		}
	}

	toDownload := manifest.Diff(local, best.remote)
	toDownload = dropFlatpakShellComponent(toDownload, edition)
	if len(toDownload) > 0 && !installUpdates && launcherInstalled {
		required := best.plan != nil && best.plan.Required
		if !required {
			notify(cb, StatusUpdateAvailable, cb.tr("Доступне оновлення: %s (автооновлення вимкнено)", "Update available: %s (auto-update disabled)", best.remote.Version))
			notify(cb, StatusLaunching, cb.meme(StatusLaunching, 0))
			holdSplash(start, minSplashShow)
			return nil
		}
	}
	if installUpdates || !launcherInstalled {
		toDownload = mergeUnique(toDownload, missingRequiredComponents(installDir, best.remote))
	}
	inFlatpak := runningInFlatpak()
	toDownload = filterFlatpakBundle(toDownload, inFlatpak)
	if len(toDownload) == 0 {
		notify(cb, StatusUpToDate, cb.tr("Лаунчер актуальний", "Launcher is up to date"))
		holdSplash(start, minSplashShow)
		return nil
	}

	if err := eng.Lock(); err != nil {
		return launchCurrent(cb.tr("Інше оновлення вже виконується", "Another update is already running"))
	}
	if err := eng.CheckDiskSpace(toDownload); err != nil {
		return launchCurrent(err.Error())
	}

	var totalBytes int64
	for _, c := range toDownload {
		totalBytes += c.Size
	}
	if cb.OnUpdateAvailable != nil {
		cb.OnUpdateAvailable(best.remote.Version, totalBytes, len(toDownload))
	}
	notify(cb, StatusUpdateAvailable, cb.tr("Доступне оновлення: %s", "Update available: %s", best.remote.Version))
	notify(cb, StatusDownloading, cb.meme(StatusDownloading, 0))

	tmpDir, err := os.MkdirTemp(installDir, ".update-tmp-")
	if err != nil {
		return launchCurrent(err.Error())
	}
	defer os.RemoveAll(tmpDir)
	downloaded, err := download.DownloadWith(ctx, best.baseURL, best.token, tmpDir, toDownload, cb.Lang, func(p download.Progress) {
		if cb.OnProgress != nil {
			cb.OnProgress(p)
		}
	}, best.allowRedirect)
	if err != nil {
		return launchCurrent(cb.tr("Не вдалося завантажити оновлення — запускаємо поточну версію", "Couldn't download the update — launching the current version"))
	}

	// Authenticode — другий, незалежний від маніфесту шар (§5.6).
	if best.plan != nil && best.plan.Authenticode && runtime.GOOS == "windows" {
		for _, dc := range downloaded {
			ext := strings.ToLower(filepath.Ext(dc.Path))
			if ext != ".exe" && ext != ".dll" {
				continue
			}
			if err := authenticode.Verify(dc.TempPath, best.plan.Signers); err != nil {
				return launchCurrent(cb.tr("Оновлення відхилено: підпис файлу ", "Update rejected: file signature ") + dc.Path + ": " + err.Error())
			}
		}
	}

	notify(cb, StatusInstalling, cb.meme(StatusInstalling, 0))
	// Flatpak: новий пакет ставить flatpak на хості, потім лаунчер перезапускається.
	if inFlatpak {
		for _, dc := range downloaded {
			if !isFlatpakBundle(dc.Component) {
				continue
			}
			info, err := installFlatpakBundle(ctx, dc.TempPath)
			if err != nil {
				return launchCurrent(cb.tr("Не вдалося встановити оновлення Flatpak: ", "Couldn't install the Flatpak update: ") + err.Error())
			}
			eng.RecordInstalled(best.remote.Version)
			notify(cb, StatusLaunching, cb.tr("Оновлено до %s — перезапуск…", "Updated to %s — restarting…", best.remote.Version))
			if err := relaunchFlatpak(info); err != nil {
				return launchCurrent(cb.tr("Оновлення встановлено, перезапустіть лаунчер", "Update installed, please restart the launcher"))
			}
			eng.Close() // стан і lock-файл — до виходу (defer не спрацює після os.Exit)
			time.Sleep(launchPause)
			os.Exit(0)
		}
	}
	if best.plan != nil {
		if err := eng.BeforeApply(best.plan); err != nil {
			return launchCurrent(err.Error())
		}
	}
	if err := install.Apply(ctx, installDir, downloaded, best.remote, cb.Lang, func(msg string) {
		notify(cb, StatusInstalling, msg)
	}); err != nil {
		notify(cb, StatusError, err.Error())
		return err
	}
	if best.plan != nil {
		applied := make([]manifest.Component, 0, len(downloaded))
		for _, dc := range downloaded {
			applied = append(applied, dc.Component)
		}
		_ = eng.AfterApply(best.plan, applied)
	} else {
		eng.RecordInstalled(best.remote.Version)
	}

	notify(cb, StatusLaunching, cb.meme(StatusLaunching, 0))
	time.Sleep(launchPause)
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
