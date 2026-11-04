package main

// bootstrap_github.go — початкова качка/відновлення компонентів у ЄДИНОМУ
// лаунчері (без токена CDN). Джерело — GitHub Releases, довіра — маніфест v2,
// підписаний release-ключем із keyset, який підписано вшитим root-ключем.
// Той самий контракт, що в оновлювачі (updater-backend/internal/newupdate);
// адреси будує сам shell із репозиторію в keyset і тега з маніфесту.
//
// Звичайно setup єдиного лаунчера ставить усі компоненти сам (-Bundle), тож
// цей шлях потрібен лише, якщо компоненти зникли (антивірус, ручне видалення).

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shaurma-trust"
)

// Вшиваються при збірці разом із rootKeys: -X main.embeddedKeyset=<base64 keyset.json>
// -X main.embeddedKeysetSig=<підпис root>.
var (
	embeddedKeyset    = ""
	embeddedKeysetSig = ""
)

// ghReleaseBase — https://github.com/<repo>/releases/download/<tag>, коли
// качаємо з GitHub (порожньо — старий CDN). Використовують urlOf,
// checkDownloadURL і клієнт завантаження.
var ghReleaseBase = ""

// ghAssetHosts — куди GitHub дозволено перенаправляти ассети релізу.
var ghAssetHosts = []string{"github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com"}

// githubBase — підміняється в тестах.
var githubBase = "https://github.com"

func ghClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("забагато редиректів")
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("редирект на небезпечну схему %q відхилено", req.URL.Scheme)
		}
		h := strings.ToLower(req.URL.Hostname())
		for _, a := range ghAssetHosts {
			if h == a {
				return nil
			}
		}
		return fmt.Errorf("редирект на недозволений хост %q відхилено", h)
	}}
}

func ghGet(rawURL string, max int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	resp, err := ghClient(60 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, rawURL)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("відповідь завелика")
	}
	return data, nil
}

// latestSigned — останній ассет релізу і його .sig (latest — без ліміту API).
func latestSigned(repo, name string) ([]byte, string, error) {
	base := githubBase + "/" + repo + "/releases/latest/download/"
	data, err := ghGet(base+url.PathEscape(name), trust.MaxDocSize)
	if err != nil {
		return nil, "", err
	}
	sig, err := ghGet(base+url.PathEscape(name+trust.SigSuffix), 4096)
	if err != nil {
		return nil, "", err
	}
	return data, strings.TrimSpace(string(sig)), nil
}

// githubManifest — перевірений маніфест останнього стабільного релізу у
// форматі bootstrapRemoteManifest (роль shell → ім'я поточного exe).
func githubManifest() (*bootstrapRemoteManifest, error) {
	roots := trust.ParsePublicKeys(rootKeys)
	if len(roots) == 0 {
		return nil, errors.New("root-ключ не вшито")
	}
	raw, err := base64.StdEncoding.DecodeString(embeddedKeyset)
	if err != nil {
		return nil, err
	}
	ks, err := trust.ParseKeyset(roots, raw, embeddedKeysetSig)
	if err != nil {
		return nil, fmt.Errorf("вшитий keyset: %w", err)
	}
	// Новіший keyset з релізу (ротація ключів) — лише з підписом root.
	if kraw, ksig, err := latestSigned(ks.Sources.Repo, trust.KeysetAsset); err == nil {
		if nks, err := trust.ParseKeyset(roots, kraw, ksig); err == nil && nks.Version > ks.Version {
			ks = nks
		}
	}
	payload, sig, err := latestSigned(ks.Sources.Repo, trust.ManifestAsset)
	if err != nil {
		return nil, fmt.Errorf("маніфест релізу: %w", err)
	}
	now := time.Now().UTC()
	m, err := trust.ParseManifest(ks, payload, sig, now)
	if err != nil {
		return nil, err
	}
	if err := trust.CheckUpdate(m, ks, trust.UpdatePolicy{Channel: "stable", Now: now}); err != nil {
		return nil, err
	}
	for _, h := range ks.Sources.AssetHosts {
		ghAssetHosts = append(ghAssetHosts, h)
	}
	ghReleaseBase = githubBase + "/" + ks.Sources.Repo + "/releases/download/" + url.PathEscape(m.Tag)

	selfExe := ""
	if exe, err := os.Executable(); err == nil {
		selfExe = filepath.Base(exe)
	}
	out := &bootstrapRemoteManifest{Version: m.Version, ReleaseNotes: m.Notes}
	for _, f := range m.FilesFor(runtime.GOOS + "-" + runtime.GOARCH) {
		path := f.Path
		switch f.Role {
		case trust.FileBootstrap:
			continue
		case trust.FileShell:
			if selfExe != "" {
				path = selfExe
			}
		}
		out.Components = append(out.Components, bootstrapComponent{
			Path: path, URL: "/" + f.Name, SHA256: strings.ToLower(f.SHA256), Size: f.Size, Kind: f.Kind, ExtractTo: f.ExtractTo,
		})
	}
	if len(out.Components) == 0 {
		return nil, errors.New("у маніфесті немає файлів для цієї платформи")
	}
	return out, nil
}

// bootstrapFromGitHub — качка всіх компонентів з GitHub за маніфестом v2.
func bootstrapFromGitHub() error {
	shellEmit("updater:status", `{"status":"checking","message":"Перевірка оновлень..."}`)
	m, err := githubManifest()
	if err != nil {
		bootstrapLog(exeDir(), "МАНІФЕСТ GitHub ВІДХИЛЕНО: %v", err)
		return fmt.Errorf("не вдалося отримати перевірений маніфест: %v", err)
	}
	if err := validateManifestArchitecture(m); err != nil {
		return err
	}
	data, _ := json.Marshal(m.Components)
	bootstrapLog(exeDir(), "GitHub %s: %d компонентів (%d байт опису)", m.Version, len(m.Components), len(data))
	shellEmit("updater:status", `{"status":"downloading","message":"Завантаження компонентів лаунчера..."}`)
	return bootstrapAll(exeDir(), m)
}
