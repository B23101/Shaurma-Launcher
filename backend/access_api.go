package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/access"
	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/creds"
)

// Доступ до приватних збірок (план «Єдиний лаунчер», §3.7): Налаштування →
// «Доступ». Активація файлом, перетягуванням або вставленим текстом,
// відв'язування пристрою. Подія access:changed — розділ збірок з'являється
// чи зникає без перезапуску.

var (
	accessOnce sync.Once
	accessMgr  *access.Manager
)

// accessManager — лінива ініціалізація (installDir відомий лише після cfg).
func (a *App) accessManager() *access.Manager {
	accessOnce.Do(func() {
		var extra []string
		if exe, err := os.Executable(); err == nil {
			if d := filepath.Dir(exe); d != a.cfg.InstallDir() {
				extra = append(extra, d)
			}
		}
		accessMgr = access.New(a.cfg.InstallDir(), readLauncherVersion(a.cfg.InstallDir()), extra...)
		accessMgr.OnChange(func(s access.Status) {
			a.emit("access:changed", s)
			a.invalidatePacksAfterAccessChange()
		})
		creds.SetEntitlementProvider(accessMgr.Entitlement)
	})
	return accessMgr
}

// startAccess — фонова активація при старті (cert біля exe чи в теці, §3.2).
func (a *App) startAccess() {
	m := a.accessManager()
	go func() {
		defer crashguard.Recover("access_api.go:startAccess")
		m.AutoActivate(context.Background())
	}()
}

func (a *App) invalidatePacksAfterAccessChange() {
	if a.packs != nil {
		a.packs.InvalidatePackCache()
	}
}

// GetAccessStatus — стан для розділу «Доступ».
func (a *App) GetAccessStatus() access.Status { return a.accessManager().Status() }

// PickAndActivateAccess — «Вибрати файл сертифіката» (файловий діалог).
func (a *App) PickAndActivateAccess() (access.Status, error) {
	path := bridgeDialogOpenFile("Виберіть файл сертифіката", false, map[string][]string{
		"Сертифікат ШАУРМА (*.cert)": {"*.cert"},
	})
	if path == "" {
		return a.GetAccessStatus(), nil
	}
	return a.ActivateAccessFromFile(path)
}

// ActivateAccessFromFile — файл, перетягнутий у вікно, чи вибраний у діалозі.
func (a *App) ActivateAccessFromFile(path string) (access.Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return a.accessManager().ActivateFromFile(ctx, path)
}

// ActivateAccessFromText — запасний варіант: вміст сертифіката текстом.
func (a *App) ActivateAccessFromText(text string) (access.Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return a.accessManager().ActivateFromText(ctx, text)
}

// UnlinkAccessDevice — «Відв'язати цей пристрій».
func (a *App) UnlinkAccessDevice() (access.Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.accessManager().Unlink(ctx)
}
