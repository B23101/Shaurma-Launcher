package minecraft

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/prismmeta"
)

// LoaderValidator перевіряє існування версій лоадерів ПЕРЕД встановленням.
// Портовано з Prism Launcher: Meta::VersionList::hasVersion() та
// ComponentUpdateTask::loadComponent() — валідація через мета-API.
//
// Prism завжди перевіряє існування версії в мета-репозиторії перед тим, як
// намагатися її завантажити. Якщо версія не існує — показує зрозумілу помилку
// "версія X не знайдена для Minecraft Y", а не криптичний HTTP 404 при спробі
// quality installer.jar чи профіль з мети.
type LoaderValidator struct {
	client *http.Client
	prism  *prismmeta.Client

	// fetchMu/fetchCache — кеш відповідей мета-API (списки версій
	// Fabric/Quilt, maven-metadata.xml) з TTL 30 хв — як у prismmeta.Client
	// і як описано в docs/dev/LOADER_VALIDATION.md: «Кеш валідації — зберігати
	// результати локально (30 хв TTL)». Списки версій міняються рідко, а
	// валідація стоїть на гарячому шляху встановлення лоадера — повторні
	// мережеві запити не потрібні, і перевірка працює з кешу навіть без
	// мережі (офлайн-режим у межах TTL). Кешуються лише УСПІШНІ відповіді:
	// при мережевій помилці наступна спроба знову йде в мережу.
	fetchMu    sync.Mutex
	fetchCache map[string]cachedFetch
}

// cachedFetch — кешоване тіло однієї мета-відповіді.
type cachedFetch struct {
	body []byte
	at   time.Time
}

// validatorFetchTTL — термін життя кешу мета-відповідей валідатора.
const validatorFetchTTL = 30 * time.Minute

func NewLoaderValidator() *LoaderValidator {
	return &LoaderValidator{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		prism:      prismmeta.NewClient(),
		fetchCache: map[string]cachedFetch{},
	}
}

// ValidateLoaderVersion перевіряє, чи існує вказана версія лоадера для
// вказаної версії Minecraft. Повертає nil, якщо версія існує, або помилку
// з детальним поясненням проблеми.
//
// Це аналог Prism Meta::VersionList::getVersion() + BaseEntity::loadTask() —
// перевірка існування через список версій з мета-API.
func (v *LoaderValidator) ValidateLoaderVersion(loader, mcVersion, loaderVersion string) error {
	switch strings.ToLower(loader) {
	case "vanilla", "":
		// Ванілла не має версії лоадера — завжди валідна
		return nil
	case "fabric":
		return v.validateFabricVersion(mcVersion, loaderVersion)
	case "quilt":
		return v.validateQuiltVersion(mcVersion, loaderVersion)
	case "forge":
		return v.validateForgeVersion(mcVersion, loaderVersion)
	case "neoforge":
		return v.validateNeoForgeVersion(mcVersion, loaderVersion)
	default:
		return fmt.Errorf("непідтримуваний лоадер: %s", loader)
	}
}

// validateFabricVersion перевіряє існування версії Fabric через мета-API.
// Fabric Meta API: GET /v2/versions/loader/{mcVersion} повертає список
// всіх версій лоадера для цієї версії Minecraft.
func (v *LoaderValidator) validateFabricVersion(mcVersion, loaderVersion string) error {
	if loaderVersion == "" || loaderVersion == "recommended" || loaderVersion == "latest" {
		// Рекомендована/найновіша версія завжди існує (береться з мети)
		return nil
	}

	// Пробуємо всі fallback URLs
	for _, baseURL := range fabricMetaURLs {
		url := baseURL + mcVersion
		body, err := v.fetchWithRetry(url, 2)
		if err != nil {
			continue // пробуємо наступний fallback
		}

		var entries []struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}
		if err := json.Unmarshal(body, &entries); err != nil {
			continue
		}

		// Перевіряємо, чи є наша версія у списку
		for _, e := range entries {
			if e.Loader.Version == loaderVersion {
				return nil // версія знайдена
			}
		}

		// Якщо дійшли сюди — версія не знайдена
		return v.buildLoaderNotFoundError("Fabric", mcVersion, loaderVersion, entries)
	}

	return fmt.Errorf("не вдалося перевірити існування Fabric %s для Minecraft %s (мережева помилка)", loaderVersion, mcVersion)
}

// validateQuiltVersion перевіряє існування версії Quilt (той самий протокол, що Fabric).
func (v *LoaderValidator) validateQuiltVersion(mcVersion, loaderVersion string) error {
	if loaderVersion == "" || loaderVersion == "recommended" || loaderVersion == "latest" {
		return nil
	}

	for _, baseURL := range quiltMetaURLs {
		url := baseURL + mcVersion
		body, err := v.fetchWithRetry(url, 2)
		if err != nil {
			continue
		}

		var entries []struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}
		if err := json.Unmarshal(body, &entries); err != nil {
			continue
		}

		for _, e := range entries {
			if e.Loader.Version == loaderVersion {
				return nil
			}
		}

		return v.buildLoaderNotFoundError("Quilt", mcVersion, loaderVersion, entries)
	}

	return fmt.Errorf("не вдалося перевірити існування Quilt %s для Minecraft %s (мережева помилка)", loaderVersion, mcVersion)
}

// validateForgeVersion перевіряє існування версії Forge. Джерело правди —
// мета-сервер Prism Launcher (компонент net.minecraftforge): сумісність з
// MC вирішується через requires.equals, а не префіксну хевристику
// "<mc>-<ver>". Це той самий шлях, що Prism Meta::VersionList::hasVersion().
// Фолбек на maven-metadata.xml Forge, якщо Prism-meta недоступний.
func (v *LoaderValidator) validateForgeVersion(mcVersion, loaderVersion string) error {
	if loaderVersion == "" || loaderVersion == "recommended" || loaderVersion == "latest" {
		// Рекомендована/найновіша версія завжди існує (береться з мети)
		return nil
	}

	entries, err := v.prism.Index(prismmeta.UIDForge)
	if err == nil {
		compatible := prismmeta.FilterByMinecraft(entries, mcVersion)
		for _, e := range compatible {
			if e.Version == loaderVersion {
				return nil // версія знайдена
			}
		}
		if len(compatible) == 0 {
			return fmt.Errorf("Forge не підтримує Minecraft %s (версія %s не знайдена у Prism-meta)", mcVersion, loaderVersion)
		}
		return v.buildPrismLoaderNotFoundError("Forge", mcVersion, loaderVersion, compatible)
	}

	// Фолбек: maven-metadata.xml (старий шлях).
	return v.validateForgeVersionMaven(mcVersion, loaderVersion)
}

// validateForgeVersionMaven — колишній шлях валідації Forge через
// maven-metadata.xml (префікс "<mcVersion>-<forgeVersion>").
func (v *LoaderValidator) validateForgeVersionMaven(mcVersion, loaderVersion string) error {
	if loaderVersion == "" || loaderVersion == "recommended" || loaderVersion == "latest" {
		return nil
	}

	fullVersion := mcVersion + "-" + loaderVersion

	for _, baseURL := range forgeMavenURLs {
		url := baseURL + "maven-metadata.xml"
		body, err := v.fetchWithRetry(url, 2)
		if err != nil {
			continue
		}

		var meta struct {
			Versioning struct {
				Versions struct {
					Version []string `xml:"version"`
				} `xml:"versions"`
			} `xml:"versioning"`
		}
		if err := xml.Unmarshal(body, &meta); err != nil {
			continue
		}

		// Шукаємо повну версію у списку
		for _, v := range meta.Versioning.Versions.Version {
			if v == fullVersion {
				return nil
			}
		}

		// Збираємо доступні версії для цієї MC
		var available []string
		prefix := mcVersion + "-"
		for _, v := range meta.Versioning.Versions.Version {
			if strings.HasPrefix(v, prefix) {
				forgeVer := strings.TrimPrefix(v, prefix)
				// Відкидаємо версії з суфіксами (старі branch-мітки)
				if !strings.Contains(forgeVer, "-") {
					available = append(available, forgeVer)
				}
			}
		}

		if len(available) == 0 {
			return fmt.Errorf("Forge не підтримує Minecraft %s (версія %s не знайдена у Maven)", mcVersion, loaderVersion)
		}

		// Показуємо останні 5 доступних версій
		if len(available) > 5 {
			available = available[len(available)-5:]
		}
		return fmt.Errorf("Forge версія %s не існує для Minecraft %s.\nОстанні доступні версії: %s",
			loaderVersion, mcVersion, strings.Join(available, ", "))
	}

	return fmt.Errorf("не вдалося перевірити існування Forge %s для Minecraft %s (мережева помилка)", loaderVersion, mcVersion)
}

// validateNeoForgeVersion перевіряє існування версії NeoForge через
// мета-сервер Prism Launcher (компонент net.neoforged).
//
// ВАЖЛИВО: це виправляє реальний баг префіксної хевристики — NeoForge для
// Minecraft 1.20.1 нумерується 47.1.x, а старе правило "1.21.1 → шукати
// 21.1.*" для 1.20.1 шукало 20.1.* і ніколи не знаходило версію, тому
// валідація/встановлення NeoForge на 1.20.1 були неможливі. Prism-meta
// вирішує сумісність через requires.equals=1.20.1.
// Фолбек на maven-metadata.xml, якщо Prism-meta недоступний.
func (v *LoaderValidator) validateNeoForgeVersion(mcVersion, loaderVersion string) error {
	if loaderVersion == "" || loaderVersion == "recommended" || loaderVersion == "latest" {
		return nil
	}

	entries, err := v.prism.Index(prismmeta.UIDNeoForge)
	if err == nil {
		compatible := prismmeta.FilterByMinecraft(entries, mcVersion)
		for _, e := range compatible {
			if e.Version == loaderVersion {
				return nil // версія знайдена
			}
		}
		if len(compatible) == 0 {
			return fmt.Errorf("NeoForge не підтримує Minecraft %s (версія %s не знайдена у Prism-meta)", mcVersion, loaderVersion)
		}
		return v.buildPrismLoaderNotFoundError("NeoForge", mcVersion, loaderVersion, compatible)
	}

	// Фолбек: maven-metadata.xml (старий шлях).
	return v.validateNeoForgeVersionMaven(mcVersion, loaderVersion)
}

// validateNeoForgeVersionMaven — колишній шлях валідації NeoForge через
// maven-metadata.xml. У NeoForge ДВІ лінійки публікації:
//
//	· 1.20.1 (47.x) — артефакт "forge" з mc-префіксованими версіями
//	  (1.20.1-47.1.106): net/neoforged/forge/maven-metadata.xml;
//	· 1.20.2+ (20.2.x/21.x) — артефакт "neoforge" без префікса
//	  (21.1.95): net/neoforged/neoforge/maven-metadata.xml.
//
// Для кожної лінійки перевіряємо СВІЙ метадані (інакше 1.20.1 дасть
// хибне "не підтримує", бо в neoforge-метаданих немає 47.x).
func (v *LoaderValidator) validateNeoForgeVersionMaven(mcVersion, loaderVersion string) error {
	if loaderVersion == "" || loaderVersion == "recommended" || loaderVersion == "latest" {
		return nil
	}

	legacy := neoforgeUsesForgeArtifact(mcVersion, loaderVersion)
	// Спершу — metadata артефакту, під яким живе ця лінійка, потім інший.
	var bases []string
	if legacy {
		bases = []string{neoMavenForge, neoMaven}
	} else {
		bases = []string{neoMaven, neoMavenForge}
	}

	// Префікс версій для цієї MC:
	//   1.20.1 -> "1.20.1-" (mc-префіксовані forge-версії)
	//   1.21.1 -> "21.1."  (NeoForge відкидає провідний "1.")
	var prefix string
	if legacy {
		prefix = mcVersion + "-"
	} else {
		parts := strings.SplitN(mcVersion, ".", 2)
		prefix = mcVersion
		if len(parts) == 2 {
			prefix = parts[1] + "."
		}
	}

	var lastAvailable []string
	parsedAny := false
	for _, baseURL := range bases {
		url := baseURL + "maven-metadata.xml"
		body, err := v.fetchWithRetry(url, 2)
		if err != nil {
			continue
		}

		var meta struct {
			Versioning struct {
				Versions struct {
					Version []string `xml:"version"`
				} `xml:"versions"`
			} `xml:"versioning"`
		}
		if err := xml.Unmarshal(body, &meta); err != nil {
			continue
		}
		parsedAny = true

		found := false
		var available []string
		for _, ver := range meta.Versioning.Versions.Version {
			if !strings.HasPrefix(ver, prefix) {
				continue
			}
			available = append(available, ver)
			if ver == loaderVersion || ver == mcVersion+"-"+loaderVersion {
				found = true
			}
		}
		if found {
			return nil
		}
		if len(available) > 0 {
			lastAvailable = available
			// Версії цієї лінійки є, але шуканої серед них немає — це точно
			// "немає такої версії", а не "не той артефакт": виходимо з
			// чіткою помилкою (перелік доступних узятий з правильного
			// метаданих).
			break
		}
		// У цьому артефакті немає версій цієї MC — пробуємо наступний base.
	}

	if !parsedAny {
		// Жоден metadata не вдалось отримати/розпарсити — це мережева
		// проблема, а НЕ "не підтримує": повертаємо чесну помилку, щоб UI
		// не вводив користувача в оману, коли, наприклад, заблокований доступ.
		return fmt.Errorf("не вдалося перевірити існування NeoForge %s для Minecraft %s (мережева помилка)", loaderVersion, mcVersion)
	}
	if len(lastAvailable) == 0 {
		return fmt.Errorf("NeoForge не підтримує Minecraft %s (версія %s не знайдена у Maven)", mcVersion, loaderVersion)
	}
	// Показуємо останні 5 доступних версій
	if len(lastAvailable) > 5 {
		lastAvailable = lastAvailable[len(lastAvailable)-5:]
	}
	return fmt.Errorf("NeoForge версія %s не існує для Minecraft %s.\nОстанні доступні версії: %s",
		loaderVersion, mcVersion, strings.Join(lastAvailable, ", "))
}

// buildPrismLoaderNotFoundError створює детальне повідомлення про помилку
// з переліком доступних версій, взятих з Prism-meta індексу (як у Prism:
// "версія X не існує для Minecraft Y. Доступні версії: ...").
func (v *LoaderValidator) buildPrismLoaderNotFoundError(loaderName, mcVersion, requestedVersion string, compatible []prismmeta.IndexEntry) error {
	n := len(compatible)
	if n > 5 {
		n = 5
	}
	names := make([]string, 0, n)
	for _, e := range compatible[:n] {
		names = append(names, e.Version)
	}
	return fmt.Errorf("%s версія %s не існує для Minecraft %s.\nОстанні доступні версії: %s",
		loaderName, requestedVersion, mcVersion, strings.Join(names, ", "))
}

// buildLoaderNotFoundError створює детальне повідомлення про помилку
// з переліком доступних версій (як у Prism).
func (v *LoaderValidator) buildLoaderNotFoundError(loaderName, mcVersion, requestedVersion string, entries interface{}) error {
	// Витягаємо список доступних версій
	var available []string

	switch loaderName {
	case "Fabric":
		if e, ok := entries.([]struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}); ok {
			for _, entry := range e {
				available = append(available, entry.Loader.Version)
			}
		}
	case "Quilt":
		if e, ok := entries.([]struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}); ok {
			for _, entry := range e {
				available = append(available, entry.Loader.Version)
			}
		}
	}

	if len(available) == 0 {
		return fmt.Errorf("%s не підтримує Minecraft %s (версія %s не знайдена)", loaderName, mcVersion, requestedVersion)
	}

	// Показуємо перші 5 доступних версій (найновіші)
	if len(available) > 5 {
		available = available[:5]
	}

	return fmt.Errorf("%s версія %s не існує для Minecraft %s.\nДоступні версії: %s",
		loaderName, requestedVersion, mcVersion, strings.Join(available, ", "))
}

// fetchWithRetry виконує HTTP-запит з retry логікою. Успішні відповіді
// кешуються на validatorFetchTTL (30 хв) — повторна валідація тієї ж версії
// або іншої версії того ж Minecraft не тягне той самий список з мережі.
func (v *LoaderValidator) fetchWithRetry(url string, maxRetries int) ([]byte, error) {
	// TTL-кеш: відповідь у межах 30 хв повертається без мережі.
	v.fetchMu.Lock()
	if e, ok := v.fetchCache[url]; ok && time.Since(e.at) < validatorFetchTTL {
		body := e.body
		v.fetchMu.Unlock()
		return body, nil
	}
	v.fetchMu.Unlock()

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ShaurmaLauncher/2.0 (+validator)")

		resp, err := v.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		buf := make([]byte, 0, 65536)
		tmp := make([]byte, 8192)
		for {
			n, e := resp.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if e != nil {
				break
			}
		}
		resp.Body.Close()
		// Кешуємо лише успішну відповідь (невдалі/404 не кешуються — щоб
		// тимчасовий збій мети не «заморожував» валідацію на 30 хв).
		v.fetchMu.Lock()
		v.fetchCache[url] = cachedFetch{body: buf, at: time.Now()}
		v.fetchMu.Unlock()
		return buf, nil
	}

	return nil, lastErr
}
