package minecraft

import (
	"runtime"
	"strings"
)

// RuntimeContext — контекст виконання для визначення активності бібліотек,
// правил та нативних класифікаторів. Портовано з Prism Launcher
// (launcher/RuntimeContext.h) для забезпечення 100% сумісності логіки.
//
// Використовується для:
//   - Визначення чи бібліотека активна (Library.IsActive)
//   - Перевірки rules (Rule.Apply)
//   - Вибору нативного класифікатора (Library.GetCompatibleNative)
type RuntimeContext struct {
	// JavaArchitecture — "32" або "64" (розрядність Java)
	JavaArchitecture string
	
	// JavaRealArchitecture — реальна архітектура Java:
	// "amd64", "i386", "aarch64", "arm" тощо
	JavaRealArchitecture string
	
	// System — назва OS: "windows", "linux", "osx" (як у Mojang JSON)
	System string
}

// NewRuntimeContext створює RuntimeContext для поточної системи.
// Портовано з Prism Launcher логіки визначення платформи.
func NewRuntimeContext() *RuntimeContext {
	ctx := &RuntimeContext{
		JavaArchitecture:     detectJavaArchitecture(),
		JavaRealArchitecture: runtime.GOARCH,
		System:               detectSystem(),
	}
	return ctx
}

// detectSystem повертає назву OS у форматі Mojang (windows/linux/osx).
func detectSystem() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	case "linux":
		return "linux"
	default:
		return "linux" // fallback
	}
}

// detectJavaArchitecture визначає розрядність (32/64).
func detectJavaArchitecture() string {
	switch runtime.GOARCH {
	case "386", "arm":
		return "32"
	case "amd64", "arm64":
		return "64"
	default:
		return "64" // fallback
	}
}

// MappedJavaRealArchitecture повертає mapped архітектуру для класифікаторів.
// Портовано з Prism: RuntimeContext::mappedJavaRealArchitecture()
//
// Маппінг:
//   amd64 → x86_64
//   i386, i686 → x86
//   aarch64 → arm64
//   інше → як є
func (ctx *RuntimeContext) MappedJavaRealArchitecture() string {
	arch := ctx.JavaRealArchitecture
	
	switch arch {
	case "amd64":
		return "x86_64"
	case "386", "i386", "i686":
		return "x86"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

// GetClassifier повертає класифікатор платформи у форматі "os-arch".
// Портовано з Prism: RuntimeContext::getClassifier()
//
// Приклади:
//   linux-x86_64
//   windows-x86
//   osx-arm64
func (ctx *RuntimeContext) GetClassifier() string {
	return ctx.System + "-" + ctx.MappedJavaRealArchitecture()
}

// IsLegacyArch перевіряє чи це "legacy" архітектура (без явного classifier).
// Портовано з Prism: RuntimeContext::isLegacyArch()
//
// Legacy означає що нативна бібліотека не має окремого класифікатора для
// кожної архітектури, лише для OS (напр. "natives-linux" замість
// "natives-linux-x86_64").
func (ctx *RuntimeContext) IsLegacyArch() bool {
	// У Prism це завжди true для сумісності зі старими версіями MC
	// які не мали classifier для кожної архітектури
	return true
}

// ClassifierMatches перевіряє чи target класифікатор відповідає поточній
// платформі. Портовано з Prism: RuntimeContext::classifierMatches()
//
// Логіка (ТОЧНО як у Prism):
//   1. Спроба точного match: "linux-x86_64" == "linux-x86_64"
//   2. Спроба legacy match: "linux" == "linux" (якщо IsLegacyArch)
//
// Приклади:
//   target="linux-x86_64", система=linux-x86_64 → true
//   target="linux", система=linux-x86_64 → true (legacy)
//   target="windows", система=linux-x86_64 → false
func (ctx *RuntimeContext) ClassifierMatches(target string) bool {
	// 1. Точна відповідність "[os]-[arch]"
	if target == ctx.GetClassifier() {
		return true
	}
	
	// 2. Legacy відповідність "[os]" (без архітектури)
	if ctx.IsLegacyArch() && target == ctx.System {
		return true
	}
	
	return false
}

// SystemMatches перевіряє чи назва OS співпадає.
// Додаткова функція для спрощення перевірок.
func (ctx *RuntimeContext) SystemMatches(osName string) bool {
	return strings.EqualFold(ctx.System, osName)
}
