package minecraft

import (
	"fmt"
	"strings"
)

// LaunchProfile представляє зібраний профіль для запуску гри.
// Портовано з Prism Launcher (launcher/minecraft/LaunchProfile.h/cpp)
// для забезпечення 100% сумісності логіки підготовки запуску.
//
// LaunchProfile збирається з кількох VersionFile (компонентів):
//   1. Loader (Fabric/Quilt/Forge/NeoForge)
//   2. Vanilla Minecraft
//   3. Додаткові компоненти
//
// Кожен компонент додає свої бібліотеки, аргументи, tweakers тощо.
type LaunchProfile struct {
	// MinecraftVersion - версія Minecraft (напр. "1.20.1")
	MinecraftVersion string
	
	// MinecraftVersionType - тип: "release", "snapshot"
	MinecraftVersionType string
	
	// MinecraftAssets - інформація про assets
	MinecraftAssets *AssetIndexInfo
	
	// MinecraftArguments - аргументи запуску (legacy формат)
	MinecraftArguments string
	
	// MainClass - головний клас для запуску
	MainClass string
	
	// AppletClass - аплет клас (legacy)
	AppletClass string
	
	// Libraries - звичайні бібліотеки (з дедуплікацією)
	Libraries []*Library
	
	// NativeLibraries - нативні бібліотеки (з дедуплікацією)
	NativeLibraries []*Library
	
	// MavenFiles - Maven файли (не в classpath)
	MavenFiles []*Library
	
	// MainJar - головний JAR (minecraft.jar)
	MainJar *Library
	
	// Traits - властивості/features
	Traits map[string]bool
	
	// Tweakers - tweaker класи
	Tweakers []string
	
	// AddnJvmArguments - додаткові JVM аргументи
	AddnJvmArguments []string
	
	// Agents - Java агенти
	Agents []JavaAgent
	
	// CompatibleJavaMajors - сумісні Java версії
	CompatibleJavaMajors []int
	
	// JarMods - jar модифікації
	JarMods []*Library
}

// NewLaunchProfile створює порожній профіль.
func NewLaunchProfile() *LaunchProfile {
	return &LaunchProfile{
		Libraries:            make([]*Library, 0),
		NativeLibraries:      make([]*Library, 0),
		MavenFiles:           make([]*Library, 0),
		Traits:               make(map[string]bool),
		Tweakers:             make([]string, 0),
		AddnJvmArguments:     make([]string, 0),
		Agents:               make([]JavaAgent, 0),
		CompatibleJavaMajors: make([]int, 0),
		JarMods:              make([]*Library, 0),
	}
}

// ApplyVersionFile застосовує VersionFile до профілю.
// Портовано з Prism: VersionFile::applyTo(LaunchProfile*)
//
// Логіка (ТОЧНО як у Prism):
//   1. Застосування базових властивостей (version, mainClass, assets)
//   2. Застосування аргументів
//   3. Застосування tweakers
//   4. Застосування бібліотек (з дедуплікацією)
//   5. Застосування traits, агентів тощо
func (lp *LaunchProfile) ApplyVersionFile(vf *VersionFile, ctx *RuntimeContext) {
	// 1. Версія Minecraft
	if vf.IsMinecraftComponent() && vf.ID != "" {
		lp.MinecraftVersion = vf.ID
		lp.MinecraftVersionType = vf.Type
	}
	
	// 2. Assets
	if vf.Assets != "" {
		lp.MinecraftAssets = vf.AssetIndex
	}
	
	// 3. Main JAR
	if vf.MainJar != nil {
		lp.MainJar = vf.MainJar
	}
	
	// 4. MainClass
	if vf.MainClass != "" {
		lp.MainClass = vf.MainClass
	}
	
	// 5. AppletClass (legacy)
	if vf.AppletClass != "" {
		lp.AppletClass = vf.AppletClass
	}
	
	// 6. Аргументи (legacy формат)
	if vf.MinecraftArguments != "" {
		lp.MinecraftArguments = vf.MinecraftArguments
	}
	
	// 7. Додаткові JVM аргументи
	for _, arg := range vf.AddnJvmArguments {
		lp.AddnJvmArguments = append(lp.AddnJvmArguments, arg)
	}
	
	// 8. Tweakers (з дедуплікацією)
	for _, tweaker := range vf.Tweakers {
		lp.ApplyTweaker(tweaker)
	}
	
	// 9. Traits
	for _, trait := range vf.Traits {
		lp.Traits[trait] = true
	}
	
	// 10. Бібліотеки (з дедуплікацією та перевіркою активності)
	for _, lib := range vf.Libraries {
		if lib.IsActive(ctx) {
			lp.ApplyLibrary(lib, ctx)
		}
	}
	
	// 11. Нативні бібліотеки
	for _, lib := range vf.NativeLibraries {
		if lib.IsActive(ctx) {
			lp.ApplyNativeLibrary(lib, ctx)
		}
	}
	
	// 12. Maven файли
	for _, lib := range vf.MavenFiles {
		if lib.IsActive(ctx) {
			lp.ApplyMavenFile(lib, ctx)
		}
	}
	
	// 13. Java агенти
	for _, agent := range vf.Agents {
		lp.Agents = append(lp.Agents, agent)
	}
	
	// 14. Jar mods
	for _, jarMod := range vf.JarMods {
		lp.JarMods = append(lp.JarMods, jarMod)
	}
	
	// 15. Compatible Java majors
	for _, major := range vf.GetCompatibleJavaMajors() {
		lp.ApplyCompatibleJavaMajor(major)
	}
}

// ApplyTweaker додає tweaker з дедуплікацією.
// Портовано з Prism: LaunchProfile::applyTweakers()
func (lp *LaunchProfile) ApplyTweaker(tweaker string) {
	// Перевірка чи вже є
	for _, t := range lp.Tweakers {
		if t == tweaker {
			return
		}
	}
	lp.Tweakers = append(lp.Tweakers, tweaker)
}

// ApplyLibrary додає звичайну бібліотеку з дедуплікацією.
// Портовано з Prism: LaunchProfile::applyLibrary()
//
// Логіка дедуплікації (ТОЧНО як у Prism):
//   1. Шукаємо бібліотеку з таким же group:artifact
//   2. Якщо не знайдено → додаємо
//   3. Якщо знайдено → порівнюємо версії, залишаємо новішу
func (lp *LaunchProfile) ApplyLibrary(lib *Library, ctx *RuntimeContext) {
	// Створюємо копію для збереження (як у Prism)
	libCopy := lib.LimitedCopy()
	
	// Шукаємо існуючу бібліотеку з таким же base name
	existing := lp.findLibraryByName(lib.RawName())
	
	if existing == nil {
		// Не знайдено → додаємо
		lp.Libraries = append(lp.Libraries, libCopy)
	} else {
		// Знайдено → порівнюємо версії
		if lib.CompareVersion(existing) {
			// Нова версія новіша → заміняємо
			*existing = *libCopy
		}
		// Інакше залишаємо стару (вона новіша або рівна)
	}
}

// ApplyNativeLibrary додає нативну бібліотеку (аналогічно ApplyLibrary).
func (lp *LaunchProfile) ApplyNativeLibrary(lib *Library, ctx *RuntimeContext) {
	libCopy := lib.LimitedCopy()
	
	existing := lp.findNativeLibraryByName(lib.RawName())
	
	if existing == nil {
		lp.NativeLibraries = append(lp.NativeLibraries, libCopy)
	} else {
		if lib.CompareVersion(existing) {
			*existing = *libCopy
		}
	}
}

// ApplyMavenFile додає Maven файл.
func (lp *LaunchProfile) ApplyMavenFile(lib *Library, ctx *RuntimeContext) {
	libCopy := lib.LimitedCopy()
	lp.MavenFiles = append(lp.MavenFiles, libCopy)
}

// ApplyCompatibleJavaMajor додає сумісну Java версію (з дедуплікацією).
func (lp *LaunchProfile) ApplyCompatibleJavaMajor(major int) {
	for _, m := range lp.CompatibleJavaMajors {
		if m == major {
			return
		}
	}
	lp.CompatibleJavaMajors = append(lp.CompatibleJavaMajors, major)
}

// findLibraryByName шукає бібліотеку за базовою назвою (group:artifact).
func (lp *LaunchProfile) findLibraryByName(rawName string) *Library {
	for _, lib := range lp.Libraries {
		if lib.RawName() == rawName {
			return lib
		}
	}
	return nil
}

// findNativeLibraryByName шукає нативну бібліотеку за базовою назвою.
func (lp *LaunchProfile) findNativeLibraryByName(rawName string) *Library {
	for _, lib := range lp.NativeLibraries {
		if lib.RawName() == rawName {
			return lib
		}
	}
	return nil
}

// GetLibraryFiles повертає списки файлів для classpath.
// Портовано з Prism: LaunchProfile::getLibraryFiles()
//
// Логіка (ТОЧНО як у Prism):
//   1. Ітерація через звичайні бібліотеки
//   2. Виклик lib.GetApplicableFiles() для кожної
//   3. Додавання mainJar
//   4. Ітерація через нативні бібліотеки
//   5. Вибір native32 або native64 залежно від архітектури
func (lp *LaunchProfile) GetLibraryFiles(
	ctx *RuntimeContext,
	basePath string,
) (jars, nativeJars []string) {
	
	// 1. Звичайні бібліотеки
	for _, lib := range lp.Libraries {
		jar, native, native32, native64 := lib.GetApplicableFiles(ctx, basePath)
		jars = append(jars, jar...)
		nativeJars = append(nativeJars, native...)
		
		// Вибір архітектури
		if ctx.JavaArchitecture == "32" {
			nativeJars = append(nativeJars, native32...)
		} else {
			nativeJars = append(nativeJars, native64...)
		}
	}
	
	// 2. Головний JAR (в кінець classpath, як у Prism)
	if lp.MainJar != nil {
		mainJarPath := lp.MainJar.ActualPath(ctx, basePath)
		jars = append(jars, mainJarPath)
	}
	
	// 3. Нативні бібліотеки
	for _, lib := range lp.NativeLibraries {
		_, native, native32, native64 := lib.GetApplicableFiles(ctx, basePath)
		nativeJars = append(nativeJars, native...)
		
		if ctx.JavaArchitecture == "32" {
			nativeJars = append(nativeJars, native32...)
		} else {
			nativeJars = append(nativeJars, native64...)
		}
	}
	
	return jars, nativeJars
}

// GetMinecraftArguments повертає аргументи запуску.
func (lp *LaunchProfile) GetMinecraftArguments() string {
	return lp.MinecraftArguments
}

// GetTweakers повертає список tweakers.
func (lp *LaunchProfile) GetTweakers() []string {
	return lp.Tweakers
}

// GetTraits повертає список traits як слайс.
func (lp *LaunchProfile) GetTraits() []string {
	traits := make([]string, 0, len(lp.Traits))
	for trait := range lp.Traits {
		traits = append(traits, trait)
	}
	return traits
}

// HasTrait перевіряє наявність trait.
func (lp *LaunchProfile) HasTrait(trait string) bool {
	return lp.Traits[trait]
}

// GetAddnJvmArguments повертає додаткові JVM аргументи.
func (lp *LaunchProfile) GetAddnJvmArguments() []string {
	return lp.AddnJvmArguments
}

// String повертає текстове представлення для діагностики.
func (lp *LaunchProfile) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("LaunchProfile{\n"))
	sb.WriteString(fmt.Sprintf("  MC Version: %s (%s)\n", lp.MinecraftVersion, lp.MinecraftVersionType))
	sb.WriteString(fmt.Sprintf("  MainClass: %s\n", lp.MainClass))
	sb.WriteString(fmt.Sprintf("  Libraries: %d\n", len(lp.Libraries)))
	sb.WriteString(fmt.Sprintf("  Natives: %d\n", len(lp.NativeLibraries)))
	sb.WriteString(fmt.Sprintf("  Tweakers: %d\n", len(lp.Tweakers)))
	sb.WriteString(fmt.Sprintf("  Traits: %v\n", lp.GetTraits()))
	sb.WriteString("}")
	return sb.String()
}
