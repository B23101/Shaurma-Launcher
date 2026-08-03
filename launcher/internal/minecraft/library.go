package minecraft

import (
	"strings"
)

// Library представляє бібліотеку Minecraft з version.json.
// Портовано з Prism Launcher (launcher/minecraft/Library.h/cpp)
// для забезпечення 100% сумісності логіки бібліотек.
//
// Підтримує:
//   - Звичайні JAR бібліотеки
//   - Нативні бібліотеки (natives-linux, natives-windows тощо)
//   - Правила активації (rules)
//   - Mojang downloads з SHA1
//   - Maven репозиторії
type Library struct {
	// Name - Maven координати (gradle specifier)
	Name *GradleSpecifier
	
	// RepositoryURL - базовий URL Maven репозиторію
	RepositoryURL string
	
	// AbsoluteURL - абсолютний URL (має пріоритет над RepositoryURL)
	AbsoluteURL string
	
	// Filename - перевизначення імені файлу (опціонально)
	Filename string
	
	// Hint - підказка: "local" (не завантажувати), "always-stale" (завжди перекачувати)
	Hint string
	
	// StoragePrefix - префікс для збереження (за замовчуванням "libraries/")
	StoragePrefix string
	
	// HasExcludes - чи є виключення при розпакуванні
	HasExcludes bool
	
	// ExtractExcludes - файли для виключення (напр. "META-INF/")
	ExtractExcludes []string
	
	// NativeClassifiers - мапа OS → classifier для нативних бібліотек
	// Приклад: {"linux": "natives-linux", "windows": "natives-windows"}
	NativeClassifiers map[string]string
	
	// Rules - правила активації
	Rules []Rule
	
	// MojangDownloads - інформація про завантаження від Mojang
	MojangDownloads *MojangLibraryDownloadInfo
}

// MojangLibraryDownloadInfo - інформація про завантаження бібліотеки.
// Портовано з Prism: MojangLibraryDownloadInfo
type MojangLibraryDownloadInfo struct {
	// Artifact - головний артефакт
	Artifact *MojangDownloadInfo `json:"artifact,omitempty"`
	
	// Classifiers - мапа classifier → download info (для natives)
	Classifiers map[string]*MojangDownloadInfo `json:"classifiers,omitempty"`
}

// MojangDownloadInfo - інформація про один файл для завантаження.
type MojangDownloadInfo struct {
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	Path string `json:"path,omitempty"`
}

// NewLibrary створює бібліотеку з Maven координат.
func NewLibrary(name string) *Library {
	return &Library{
		Name:              ParseGradleSpecifier(name),
		StoragePrefix:     "libraries/",
		NativeClassifiers: make(map[string]string),
	}
}

// IsActive перевіряє чи бібліотека активна для даного RuntimeContext.
// Портовано з Prism: Library::isActive(const RuntimeContext&)
//
// Логіка (ТОЧНО як у Prism):
//   1. Застосовуємо rules (якщо є)
//   2. Якщо rules не дозволяють → false
//   3. Для нативних бібліотек - перевіряємо наявність сумісного classifier
//   4. Якщо нативна і немає classifier → false
func (lib *Library) IsActive(ctx *RuntimeContext) bool {
	// Перевірка rules
	result := ApplyRules(lib.Rules, ctx)
	if !result {
		return false
	}
	
	// Для нативних - перевірка наявності classifier
	if lib.IsNative() {
		classifier := lib.GetCompatibleNative(ctx)
		if classifier == "" {
			return false
		}
	}
	
	return true
}

// IsNative перевіряє чи це нативна бібліотека.
// Портовано з Prism: Library::isNative()
func (lib *Library) IsNative() bool {
	return len(lib.NativeClassifiers) > 0
}

// IsLocal перевіряє чи бібліотека локальна (не завантажувати).
// Портовано з Prism: Library::isLocal()
func (lib *Library) IsLocal() bool {
	return lib.Hint == "local"
}

// IsAlwaysStale перевіряє чи завжди перекачувати.
// Портовано з Prism: Library::isAlwaysStale()
func (lib *Library) IsAlwaysStale() bool {
	return lib.Hint == "always-stale"
}

// GetCompatibleNative повертає classifier для поточної платформи.
// Портовано з Prism: Library::getCompatibleNative(const RuntimeContext&)
//
// Логіка (ТОЧНО як у Prism):
//   1. Спроба точного match "[os]-[arch]" (напр. "linux-x86_64")
//   2. Спроба legacy match "[os]" (напр. "linux")
//   3. Якщо не знайдено → ""
//
// Приклади:
//   NativeClassifiers = {"linux": "natives-linux"}
//   RuntimeContext = linux-x86_64
//   → "natives-linux" (legacy match)
//
//   NativeClassifiers = {"linux-x86_64": "natives-linux-x86_64"}
//   RuntimeContext = linux-x86_64
//   → "natives-linux-x86_64" (точний match)
func (lib *Library) GetCompatibleNative(ctx *RuntimeContext) string {
	// 1. Спроба точного match "[os]-[arch]"
	classifier := ctx.GetClassifier() // "linux-x86_64"
	if native, ok := lib.NativeClassifiers[classifier]; ok {
		return native
	}
	
	// 2. Спроба legacy match "[os]" (тільки якщо IsLegacyArch)
	if ctx.IsLegacyArch() {
		if native, ok := lib.NativeClassifiers[ctx.System]; ok {
			return native
		}
	}
	
	// 3. Не знайдено
	return ""
}

// StorageSuffix повертає шлях для збереження файлу.
// Портовано з Prism: Library::storageSuffix(const RuntimeContext&)
//
// Для звичайної: group/artifact/version/artifact-version.jar
// Для нативної: group/artifact/version/artifact-version-classifier.jar
func (lib *Library) StorageSuffix(ctx *RuntimeContext) string {
	if !lib.IsNative() {
		// Звичайна бібліотека
		return lib.Name.ToPath(lib.Filename)
	}
	
	// Нативна - потрібен classifier
	classifier := lib.GetCompatibleNative(ctx)
	if classifier == "" {
		// Немає сумісного classifier - повертаємо INVALID
		// (як у Prism, щоб побачити помилку)
		spec := *lib.Name
		spec.SetClassifier("INVALID")
		return spec.ToPath(lib.Filename)
	}
	
	// Підставляємо classifier
	spec := *lib.Name
	spec.SetClassifier(classifier)
	return spec.ToPath(lib.Filename)
}

// GetFileName повертає ім'я файлу.
// Портовано з Prism: Library::filename(const RuntimeContext&)
func (lib *Library) GetFileName(ctx *RuntimeContext) string {
	// Якщо є override - використовуємо його
	if lib.Filename != "" {
		return lib.Filename
	}
	
	if !lib.IsNative() {
		return lib.Name.GetFileName()
	}
	
	// Для нативної - з classifier
	classifier := lib.GetCompatibleNative(ctx)
	spec := *lib.Name
	spec.SetClassifier(classifier)
	return spec.GetFileName()
}

// ActualPath повертає повний шлях до файлу.
func (lib *Library) ActualPath(ctx *RuntimeContext, basePath string) string {
	suffix := lib.StorageSuffix(ctx)
	return basePath + "/" + lib.StoragePrefix + suffix
}

// GetApplicableFiles повертає списки файлів для classpath.
// Портовано з Prism: Library::getApplicableFiles()
//
// Розділяє на:
//   - jar: звичайні JAR файли
//   - native: нативні бібліотеки (universal)
//   - native32: 32-бітні нативні
//   - native64: 64-бітні нативні
//
// Логіка (ТОЧНО як у Prism):
//   1. Якщо є ${arch} в шляху → розділяємо на 32/64
//   2. Інакше → один шлях (universal)
func (lib *Library) GetApplicableFiles(
	ctx *RuntimeContext,
	basePath string,
) (jar, native, native32, native64 []string) {
	rawStorage := lib.StorageSuffix(ctx)
	
	if lib.IsNative() {
		// Перевірка чи є ${arch} placeholder
		if strings.Contains(rawStorage, "${arch}") {
			// Розділяємо на 32 та 64
			path32 := strings.ReplaceAll(rawStorage, "${arch}", "32")
			path64 := strings.ReplaceAll(rawStorage, "${arch}", "64")
			native32 = append(native32, lib.actualPathRaw(basePath, path32))
			native64 = append(native64, lib.actualPathRaw(basePath, path64))
		} else {
			// Universal native
			native = append(native, lib.actualPathRaw(basePath, rawStorage))
		}
	} else {
		// Звичайний JAR
		jar = append(jar, lib.actualPathRaw(basePath, rawStorage))
	}
	
	return
}

func (lib *Library) actualPathRaw(basePath, suffix string) string {
	return basePath + "/" + lib.StoragePrefix + suffix
}

// RawName повертає базову назву (group:artifact) для дедуплікації.
// Портовано з Prism: Library::rawName()
func (lib *Library) RawName() string {
	return lib.Name.GetArtifactBase()
}

// GetDownloadURL формує URL для завантаження.
// Портовано з Prism: Library::getDownloads()
func (lib *Library) GetDownloadURL(ctx *RuntimeContext) (url, sha1 string) {
	suffix := lib.StorageSuffix(ctx)
	
	// 1. Пріоритет: Mojang downloads
	if lib.MojangDownloads != nil {
		if lib.IsNative() {
			classifier := lib.GetCompatibleNative(ctx)
			if info, ok := lib.MojangDownloads.Classifiers[classifier]; ok {
				return info.URL, info.SHA1
			}
		} else {
			if lib.MojangDownloads.Artifact != nil {
				return lib.MojangDownloads.Artifact.URL, lib.MojangDownloads.Artifact.SHA1
			}
		}
	}
	
	// 2. Абсолютний URL
	if lib.AbsoluteURL != "" {
		return lib.AbsoluteURL, ""
	}
	
	// 3. Repository URL + suffix
	if lib.RepositoryURL != "" {
		return strings.TrimSuffix(lib.RepositoryURL, "/") + "/" + suffix, ""
	}
	
	// 4. Fallback на стандартний Maven Central
	return "https://repo1.maven.org/maven2/" + suffix, ""
}

// String повертає текстове представлення.
func (lib *Library) String() string {
	if lib.Name != nil {
		return lib.Name.Serialize()
	}
	return "<invalid library>"
}

// LimitedCopy створює копію бібліотеки для дедуплікації.
// Портовано з Prism: Library::limitedCopy()
func (lib *Library) LimitedCopy() *Library {
	copy := &Library{
		Name:              lib.Name,
		RepositoryURL:     lib.RepositoryURL,
		AbsoluteURL:       lib.AbsoluteURL,
		Filename:          lib.Filename,
		Hint:              lib.Hint,
		StoragePrefix:     lib.StoragePrefix,
		HasExcludes:       lib.HasExcludes,
		ExtractExcludes:   lib.ExtractExcludes,
		NativeClassifiers: make(map[string]string),
		Rules:             lib.Rules,
		MojangDownloads:   lib.MojangDownloads,
	}
	
	// Копіюємо мапу
	for k, v := range lib.NativeClassifiers {
		copy.NativeClassifiers[k] = v
	}
	
	return copy
}

// CompareVersion порівнює версії з іншою бібліотекою.
// Повертає true якщо ця бібліотека новіша.
func (lib *Library) CompareVersion(other *Library) bool {
	return lib.Name.CompareVersion(other.Name) > 0
}
