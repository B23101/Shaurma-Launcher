package mods

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeJar(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.jar")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	_ = f.Close()
	return path
}

func TestParsePluginYML(t *testing.T) {
	jar := writeJar(t, map[string]string{"plugin.yml": `name: LuckPerms
version: '5.5.24'
main: me.lucko.luckperms.bukkit.LPBukkitBootstrap
author: Luck
authors: [Turbo]
description: "A permissions plugin" # коментар
depend:
  - Vault
softdepend: [PlaceholderAPI]
commands:
  lp:
    description: x
`})
	m := parseJar(jar)
	if m.Name != "LuckPerms" || m.Version != "5.5.24" || m.Author != "Luck, Turbo" || m.Description != "A permissions plugin" {
		t.Errorf("метадані plugin.yml: %+v", m)
	}
	if len(m.Depends) != 1 || m.Depends[0] != "Vault" {
		t.Errorf("залежності: %v", m.Depends)
	}
	if len(m.Loaders) == 0 || m.Loaders[0] != "bukkit" || m.Environment != "server" {
		t.Errorf("платформа: %v %q", m.Loaders, m.Environment)
	}
}

func TestParseVelocityPlugin(t *testing.T) {
	jar := writeJar(t, map[string]string{"velocity-plugin.json": `{"id":"kickredirect","name":"KickRedirect","version":"2.0.10","authors":["4drian3d"],"dependencies":[{"id":"luckperms","optional":true},{"id":"limboapi","optional":false}]}`})
	m := parseJar(jar)
	if m.Name != "KickRedirect" || m.Version != "2.0.10" || m.LoaderType != "velocity" {
		t.Errorf("velocity-plugin.json: %+v", m)
	}
	if len(m.Depends) != 1 || m.Depends[0] != "limboapi" {
		t.Errorf("обов'язкові залежності: %v", m.Depends)
	}
}

// Jar для кількох платформ (Bukkit + Velocity) сумісний з кожною.
func TestMultiPlatformPlugin(t *testing.T) {
	jar := writeJar(t, map[string]string{
		"plugin.yml":           "name: SkinsRestorer\nversion: 15\n",
		"velocity-plugin.json": `{"id":"skinsrestorer","name":"SkinsRestorer","version":"15"}`,
	})
	m := parseJar(jar)
	has := map[string]bool{}
	for _, l := range m.Loaders {
		has[l] = true
	}
	if !has["paper"] || !has["velocity"] {
		t.Errorf("платформи мультиплатформного jar: %v", m.Loaders)
	}
}
