package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Тести визначення «керованої лаунчером» теки Java (isLauncherManagedJava).
//
// Фікс «java not found at <тека>\java\bin\javaw.exe»: раніше вбудована
// Java вмикалась лише при ТОЧНІЙ рівності settings.JavaPath == cfg.JavaDir().
// Якщо шляхи розходились (застаріле значення після зміни папки даних,
// незавершене перенесення в майстрі, легасі-імпорт), лаунчер вважав Java
// «ручною» і падав, не спробувавши її завантажити. Тепер будь-яка тека
// користувача без javaw.exe керується лаунчером — Java ставиться саме туди.

func TestIsLauncherManagedJava(t *testing.T) {
	dir := t.TempDir()
	cfg := newTestConfig(t, dir)
	a := &App{cfg: cfg}

	tests := []struct {
		name     string
		path     string
		setup    func() // підготовка диска (опційно)
		expected bool
	}{
		{
			name:     "порожній шлях = вбудована Java",
			path:     "",
			expected: true,
		},
		{
			name:     "рівно вбудована тека лаунчера",
			path:     cfg.JavaDir(),
			expected: true,
		},
		{
			name: "кастомна тека без javaw.exe = керується лаунчером (фікс)",
			path: filepath.Join(dir, "java-custom"),
			setup: func() {
				os.MkdirAll(filepath.Join(dir, "java-custom"), 0755)
			},
			expected: true,
		},
		{
			name: "кастомна тека з javaw.exe = ручна Java",
			path: filepath.Join(dir, "java-manual"),
			setup: func() {
				bin := filepath.Join(dir, "java-manual", "bin")
				os.MkdirAll(bin, 0755)
				os.WriteFile(filepath.Join(bin, "javaw.exe"), []byte("x"), 0644)
			},
			expected: false,
		},
		{
			name:     "явний шлях до .exe = ручна Java (навіть якщо файлу немає)",
			path:     filepath.Join(dir, "jdk-21", "bin", "javaw.exe"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			got := a.isLauncherManagedJava(tt.path)
			if got != tt.expected {
				t.Errorf("isLauncherManagedJava(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}
