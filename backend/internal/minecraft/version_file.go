package minecraft

import (
	"time"
)

// VersionFile представляє version.json файл Minecraft.
// Портовано з Prism Launcher (launcher/minecraft/VersionFile.h)
// для забезпечення 100% сумісності парсингу версій.
//
// Містить всю інформацію для запуску конкретної версії:
//   - Базові дані (ID, mainClass, аргументи)
//   - Бібліотеки (звичайні та нативні)
//   - Java вимоги
//   - Assets інформація
//   - Завантаження (client.jar, server.jar)
type VersionFile struct {
	// ─── Prism Launcher специфічні поля ───
	
	// Order - порядок застосування (для component system)
	Order int
	
	// Name - людсько-читабельна назва
	Name string
	
	// UID - унікальний ID компонента (напр. "net.minecraft", "net.fabricmc.fabric-loader")
	UID string
	
	// Version - версія компонента
	Version string
	
	// ─── Mojang поля ───
	
	// ID - ідентифікатор версії (напр. "1.20.1", "fabric-loader-0.15.0-1.20.1")
	ID string `json:"id"`
	
	// Type - тип версії: "release", "snapshot", "old_beta", "old_alpha"
	Type string `json:"type,omitempty"`
	
	// MainClass - головний клас для запуску
	// Приклад: "net.minecraft.client.main.Main" (vanilla)
	//          "net.fabricmc.loader.impl.launch.knot.KnotClient" (Fabric)
	MainClass string `json:"mainClass,omitempty"`
	
	// AppletClass - клас аплету (LEGACY, Minecraft <= 1.5.2)
	AppletClass string `json:"appletClass,omitempty"`
	
	// MinecraftArguments - аргументи запуску (LEGACY формат, string)
	// Використовується в MC <= 1.12.2
	MinecraftArguments string `json:"minecraftArguments,omitempty"`
	
	// Arguments - аргументи запуску (новий формат, MC >= 1.13)
	Arguments *ArgumentsSection `json:"arguments,omitempty"`
	
	// MinimumLauncherVersion - мінімальна версія лаунчера (Mojang перевіряє >= 18)
	MinimumLauncherVersion int `json:"minimumLauncherVersion,omitempty"`
	
	// ReleaseTime - час випуску (ISO 8601)
	ReleaseTime time.Time `json:"releaseTime,omitempty"`
	
	// Time - час останнього оновлення (ISO 8601)
	Time time.Time `json:"time,omitempty"`
	
	// ─── Assets ───
	
	// Assets - ID набору ресурсів (напр. "1.20", "legacy")
	Assets string `json:"assets,omitempty"`
	
	// AssetIndex - інформація про індекс ресурсів
	AssetIndex *AssetIndexInfo `json:"assetIndex,omitempty"`
	
	// ─── Java вимоги ───
	
	// JavaVersion - інформація про необхідну Java
	JavaVersion *JavaVersionInfo `json:"javaVersion,omitempty"`
	
	// CompatibleJavaMajors - список сумісних major версій Java (Prism extended)
	CompatibleJavaMajors []int
	
	// CompatibleJavaName - назва рекомендованої Java (Prism extended)
	CompatibleJavaName string
	
	// ─── Завантаження ───
	
	// Downloads - мапа типів завантажень (client, server, windows_server тощо)
	Downloads map[string]*MojangDownloadInfo `json:"downloads,omitempty"`
	
	// ─── Бібліотеки ───
	
	// Libraries - звичайні бібліотеки для classpath
	Libraries []*Library `json:"libraries,omitempty"`
	
	// NativeLibraries - нативні бібліотеки (внутрішнє, заповнюється при парсингу)
	NativeLibraries []*Library
	
	// MavenFiles - Maven файли (не в classpath)
	MavenFiles []*Library
	
	// MainJar - головний JAR файл (зазвичай minecraft.jar)
	MainJar *Library
	
	// JarMods - jar модифікації (LEGACY)
	JarMods []*Library
	
	// ─── Inheritance ───
	
	// InheritsFrom - батьківська версія (напр. fabric-loader наслідує від 1.20.1)
	InheritsFrom string `json:"inheritsFrom,omitempty"`
	
	// Jar - перевизначення JAR файлу (якщо відрізняється від ID)
	Jar string `json:"jar,omitempty"`
	
	// ─── Додаткові ───
	
	// Traits - набір властивостей/features
	Traits []string
	
	// Tweakers - tweaker класи (для старих лоадерів)
	Tweakers []string
	
	// Agents - Java агенти
	Agents []JavaAgent
	
	// AddnJvmArguments - додаткові JVM аргументи (з компонентів)
	AddnJvmArguments []string
	
	// Volatile - чи можна автоматично видалити цей компонент
	Volatile bool
	
	// Logging - конфігурація логування (опціонально)
	Logging interface{} `json:"logging,omitempty"`
}

// ArgumentsSection - секція аргументів (новий формат MC >= 1.13).
type ArgumentsSection struct {
	// Game - аргументи для гри
	Game []interface{} `json:"game,omitempty"`
	
	// JVM - аргументи для JVM
	JVM []interface{} `json:"jvm,omitempty"`
}

// AssetIndexInfo - інформація про індекс ресурсів.
type AssetIndexInfo struct {
	ID        string `json:"id"`
	SHA1      string `json:"sha1"`
	Size      int64  `json:"size"`
	TotalSize int64  `json:"totalSize"`
	URL       string `json:"url"`
}

// JavaVersionInfo - інформація про необхідну Java.
type JavaVersionInfo struct {
	// Component - назва Java runtime компонента
	// Приклад: "java-runtime-epsilon" (Java 25)
	Component string `json:"component,omitempty"`
	
	// MajorVersion - major версія Java (8, 17, 21, 25 тощо)
	MajorVersion int `json:"majorVersion,omitempty"`
}

// JavaAgent - Java агент (-javaagent:path=args).
type JavaAgent struct {
	Library  *Library
	Argument string
}

// NewVersionFile створює порожній VersionFile.
func NewVersionFile() *VersionFile {
	return &VersionFile{
		Downloads:         make(map[string]*MojangDownloadInfo),
		Libraries:         make([]*Library, 0),
		NativeLibraries:   make([]*Library, 0),
		MavenFiles:        make([]*Library, 0),
		JarMods:           make([]*Library, 0),
		Traits:            make([]string, 0),
		Tweakers:          make([]string, 0),
		Agents:            make([]JavaAgent, 0),
		AddnJvmArguments:  make([]string, 0),
		CompatibleJavaMajors: make([]int, 0),
	}
}

// GetMinecraftVersion повертає версію Minecraft.
// Для vanilla = ID, для лоадерів = ID батьківської версії.
func (vf *VersionFile) GetMinecraftVersion() string {
	// Якщо це компонент - повертаємо Version поле
	if vf.Version != "" {
		return vf.Version
	}
	// Інакше - ID
	return vf.ID
}

// GetMainClass повертає головний клас для запуску.
func (vf *VersionFile) GetMainClass() string {
	return vf.MainClass
}

// GetMinecraftArguments повертає аргументи запуску.
// Для legacy формату (string), для нового формату (розпарсені з Arguments).
func (vf *VersionFile) GetMinecraftArguments() string {
	return vf.MinecraftArguments
}

// HasInheritsFrom перевіряє чи є батьківська версія.
func (vf *VersionFile) HasInheritsFrom() bool {
	return vf.InheritsFrom != ""
}

// GetJarID повертає ID JAR файлу.
// Якщо Jar поле задане - повертає його, інакше InheritsFrom, інакше ID.
func (vf *VersionFile) GetJarID() string {
	if vf.Jar != "" {
		return vf.Jar
	}
	if vf.InheritsFrom != "" {
		return vf.InheritsFrom
	}
	return vf.ID
}

// IsMinecraftComponent перевіряє чи це vanilla Minecraft компонент.
func (vf *VersionFile) IsMinecraftComponent() bool {
	return vf.UID == "net.minecraft"
}

// GetCompatibleJavaMajors повертає список сумісних Java версій.
func (vf *VersionFile) GetCompatibleJavaMajors() []int {
	// Якщо є явне поле JavaVersion.MajorVersion - повертаємо його
	if vf.JavaVersion != nil && vf.JavaVersion.MajorVersion > 0 {
		return []int{vf.JavaVersion.MajorVersion}
	}
	
	// Інакше повертаємо CompatibleJavaMajors
	return vf.CompatibleJavaMajors
}

// GetJavaVersionComponent повертає назву Java компонента.
func (vf *VersionFile) GetJavaVersionComponent() string {
	if vf.JavaVersion != nil {
		return vf.JavaVersion.Component
	}
	return ""
}

// AddLibrary додає бібліотеку до відповідного списку.
// Розділяє на звичайні та нативні автоматично.
func (vf *VersionFile) AddLibrary(lib *Library) {
	if lib.IsNative() {
		vf.NativeLibraries = append(vf.NativeLibraries, lib)
	} else {
		vf.Libraries = append(vf.Libraries, lib)
	}
}

// AddTweaker додає tweaker клас (з дедуплікацією).
// Портовано з Prism: LaunchProfile::applyTweakers()
func (vf *VersionFile) AddTweaker(tweaker string) {
	// Перевірка чи вже є
	for _, t := range vf.Tweakers {
		if t == tweaker {
			return // вже є
		}
	}
	vf.Tweakers = append(vf.Tweakers, tweaker)
}

// AddTrait додає trait (з дедуплікацією).
func (vf *VersionFile) AddTrait(trait string) {
	for _, t := range vf.Traits {
		if t == trait {
			return
		}
	}
	vf.Traits = append(vf.Traits, trait)
}

// HasTrait перевіряє наявність trait.
func (vf *VersionFile) HasTrait(trait string) bool {
	for _, t := range vf.Traits {
		if t == trait {
			return true
		}
	}
	return false
}
