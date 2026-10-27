// crashai.go — ШІ-аналіз крашів Minecraft-сервера.
package servers

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// crashCache — кеш результатів аналізу (хеш файлу → відповідь).
var (
	crashCacheMu sync.RWMutex
	crashCache   = make(map[string]string)
)

// CrashAnalysis — результат ШІ-аналізу крашу.
type CrashAnalysis struct {
	HasCrash   bool   `json:"hasCrash"`
	FileName   string `json:"fileName"`
	Error      string `json:"error"`
	Analysis   string `json:"analysis"`   // відповідь ШІ
	CacheHit   bool   `json:"cacheHit"`
}

// AnalyzeServerCrash шукає останній crash-report та аналізує його через ШІ.
func AnalyzeServerCrash(serverDir string) CrashAnalysis {
	// Шукаємо crash-reports/crash-*.txt
	crashDir := filepath.Join(serverDir, "crash-reports")
	entries, err := os.ReadDir(crashDir)
	if err != nil {
		return CrashAnalysis{HasCrash: false, Error: "crash-reports директорія не знайдена"}
	}

	// Шукаємо найновіший crash файл.
	var crashFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "crash-") && strings.HasSuffix(e.Name(), ".txt") {
			crashFiles = append(crashFiles, e)
		}
	}
	if len(crashFiles) == 0 {
		return CrashAnalysis{HasCrash: false, Error: "краш-репортів не знайдено"}
	}

	// Сортуємо за часом (найновіший останній).
	sort.Slice(crashFiles, func(i, j int) bool {
		iInfo, _ := crashFiles[i].Info()
		jInfo, _ := crashFiles[j].Info()
		if iInfo == nil || jInfo == nil {
			return false
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	latest := crashFiles[0]
	filePath := filepath.Join(crashDir, latest.Name())

	// Читаємо файл (обмежуємо 50KB — ШІ не потребує весь стек).
	data, err := os.ReadFile(filePath)
	if err != nil {
		return CrashAnalysis{HasCrash: false, Error: fmt.Sprintf("читання: %v", err)}
	}
	if len(data) > 50*1024 {
		data = data[:50*1024]
	}

	content := string(data)

	// Перевіряємо кеш.
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	crashCacheMu.RLock()
	if cached, ok := crashCache[hash]; ok {
		crashCacheMu.RUnlock()
		return CrashAnalysis{
			HasCrash: true,
			FileName: latest.Name(),
			Analysis: cached,
			CacheHit: true,
		}
	}
	crashCacheMu.RUnlock()

	// Простий аналіз без зовнішнього ШІ (MVP): шукаємо типові патерни.
	analysis := analyzeCrashPatterns(content)

	// Кешуємо.
	crashCacheMu.Lock()
	crashCache[hash] = analysis
	// Прибираємо старі записи (більше 100).
	if len(crashCache) > 100 {
		for k := range crashCache {
			delete(crashCache, k)
			break
		}
	}
	crashCacheMu.Unlock()

	return CrashAnalysis{
		HasCrash: true,
		FileName: latest.Name(),
		Analysis: analysis,
		CacheHit: false,
	}
}

// analyzeCrashPatterns — локальний аналіз типових помилок.
func analyzeCrashPatterns(content string) string {
	var problems []string

	// OutOfMemoryError
	if strings.Contains(content, "OutOfMemoryError") {
		problems = append(problems, "Недостатньо оперативної пам'яті (OOM). Збільшіть Xmx.")
	}

	// Mod loading errors
	if strings.Contains(content, "ModResolutionException") || strings.Contains(content, "ModDiscoverer") {
		problems = append(problems, "Несумісний мод. Перевірте версію мода для цієї версії MC.")
	}

	// Mixin errors
	if strings.Contains(content, "MixinApplyError") || strings.Contains(content, "MixinPrepareError") {
		problems = append(problems, "Помилка Mixin. Можливо мод несумісний з поточним ядром.")
	}

	// Port already in use
	if strings.Contains(content, "Address already in use") || strings.Contains(content, "BindException") {
		problems = append(problems, "Порт вже зайнятий. Змініть порт у server.properties.")
	}

	// Java version issues
	if strings.Contains(content, "UnsupportedClassFileMajorVersion") || strings.Contains(content, "ClassFormatError") {
		problems = append(problems, "Невірна версія Java. Перевірте яку Java потребує ця версія MC/ядра.")
	}

	// Corrupted world
	if strings.Contains(content, "IOException") && strings.Contains(content, "region") {
		problems = append(problems, "Пошкоджений світ. Спробуйте відновити з бекапу.")
	}

	// Plugin issues
	if strings.Contains(content, "Could not load 'plugins") || strings.Contains(content, "Plugin") && strings.Contains(content, "Exception") {
		problems = append(problems, "Помилка плагіна. Перевірте log/latest.log для деталей.")
	}

	// Generic
	if len(problems) == 0 {
		if idx := strings.Index(content, "Exception"); idx > 0 {
			start := idx
			if start > 50 {
				start -= 50
			}
			end := idx + 200
			if end > len(content) {
				end = len(content)
			}
			problems = append(problems, fmt.Sprintf("Знайдено виключення:\n%s", strings.TrimSpace(content[start:end])))
		} else {
			problems = append(problems, "Не вдалося автоматично визначити проблему. Перевіть crash-reports вручну.")
		}
	}

	return strings.Join(problems, "\n")
}

// GetLatestCrashFile повертає шлях до останнього crash-репорту.
func GetLatestCrashFile(serverDir string) string {
	crashDir := filepath.Join(serverDir, "crash-reports")
	entries, err := os.ReadDir(crashDir)
	if err != nil {
		return ""
	}

	var latest os.DirEntry
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "crash-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latest = e
			latestTime = info.ModTime()
		}
	}
	if latest == nil {
		return ""
	}
	return filepath.Join(crashDir, latest.Name())
}

// HasCrashReport перевіряє чи є crash-reports у сервері.
func HasCrashReport(serverDir string) bool {
	crashDir := filepath.Join(serverDir, "crash-reports")
	entries, err := os.ReadDir(crashDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "crash-") {
			return true
		}
	}
	return false
}
