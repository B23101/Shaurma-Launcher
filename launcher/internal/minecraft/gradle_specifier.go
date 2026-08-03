package minecraft

import (
	"fmt"
	"regexp"
	"strings"
)

// GradleSpecifier представляє Maven/Gradle координати у форматі:
// group:artifact:version[:classifier][@extension]
//
// Портовано з Prism Launcher (launcher/minecraft/GradleSpecifier.h)
// для забезпечення 100% сумісності парсингу бібліотек.
//
// Приклади:
//   org.lwjgl:lwjgl-glfw:3.3.1
//   org.lwjgl:lwjgl-glfw:3.3.1:natives-linux
//   org.lwjgl:lwjgl-glfw:3.3.1:natives-linux@jar
//   com.mojang:patchy:1.1@jar
type GradleSpecifier struct {
	GroupID    string  // org.lwjgl
	ArtifactID string  // lwjgl-glfw
	Version    string  // 3.3.1
	Classifier string  // natives-linux (опціонально)
	Extension  string  // jar (за замовчуванням)
	Valid      bool    // чи парсинг успішний
	RawValue   string  // оригінальний рядок
}

// Регулярний вираз для парсингу (ТОЧНО як у Prism).
// Формат: group:artifact:version[:classifier][@extension]
var gradleSpecifierRegex = regexp.MustCompile(`^([^:@]+):([^:@]+):([^:@]+)(?::([^:@]+))?(?:@([^:@]+))?$`)

// ParseGradleSpecifier парсить Maven координати з рядка.
// Портовано з Prism: GradleSpecifier::GradleSpecifier(const QString&)
//
// Логіка (ТОЧНО як у Prism):
//   1. Regex match для розбиття на частини
//   2. Групи: group, artifact, version, [classifier], [extension]
//   3. За замовчуванням extension = "jar"
//   4. Якщо парсинг не вдався - Valid = false
func ParseGradleSpecifier(spec string) *GradleSpecifier {
	gs := &GradleSpecifier{
		RawValue:  spec,
		Extension: "jar", // за замовчуванням
	}
	
	matches := gradleSpecifierRegex.FindStringSubmatch(spec)
	if matches == nil {
		gs.Valid = false
		return gs
	}
	
	gs.GroupID = matches[1]
	gs.ArtifactID = matches[2]
	gs.Version = matches[3]
	
	// Classifier (опціонально)
	if len(matches) > 4 && matches[4] != "" {
		gs.Classifier = matches[4]
	}
	
	// Extension (опціонально)
	if len(matches) > 5 && matches[5] != "" {
		gs.Extension = matches[5]
	}
	
	gs.Valid = true
	return gs
}

// Serialize повертає Maven координати у вигляді рядка.
// Портовано з Prism: GradleSpecifier::serialize()
//
// Формат: group:artifact:version[:classifier][@extension]
func (gs *GradleSpecifier) Serialize() string {
	if !gs.Valid {
		return gs.RawValue
	}
	
	result := fmt.Sprintf("%s:%s:%s", gs.GroupID, gs.ArtifactID, gs.Version)
	
	if gs.Classifier != "" {
		result += ":" + gs.Classifier
	}
	
	// Extension додаємо лише якщо не "jar" (за замовчуванням)
	if gs.Extension != "" && gs.Extension != "jar" {
		result += "@" + gs.Extension
	}
	
	return result
}

// GetFileName повертає ім'я файлу для цієї бібліотеки.
// Портовано з Prism: GradleSpecifier::getFileName()
//
// Формат: artifact-version[-classifier].extension
//
// Приклади:
//   lwjgl-glfw-3.3.1.jar
//   lwjgl-glfw-3.3.1-natives-linux.jar
//   patchy-1.1.jar
func (gs *GradleSpecifier) GetFileName() string {
	if !gs.Valid {
		return ""
	}
	
	filename := gs.ArtifactID + "-" + gs.Version
	
	if gs.Classifier != "" {
		filename += "-" + gs.Classifier
	}
	
	extension := gs.Extension
	if extension == "" {
		extension = "jar"
	}
	
	filename += "." + extension
	return filename
}

// ToPath повертає шлях до файлу в Maven репозиторії.
// Портовано з Prism: GradleSpecifier::toPath()
//
// Формат: group/artifact/version/filename
//
// Приклад:
//   org.lwjgl:lwjgl-glfw:3.3.1:natives-linux
//   → org/lwjgl/lwjgl-glfw/3.3.1/lwjgl-glfw-3.3.1-natives-linux.jar
//
// Параметр filenameOverride дозволяє замінити ім'я файлу (якщо задано).
func (gs *GradleSpecifier) ToPath(filenameOverride string) string {
	if !gs.Valid {
		return ""
	}
	
	// group.id → group/id
	path := strings.ReplaceAll(gs.GroupID, ".", "/")
	
	// Додаємо artifact/version/
	path += "/" + gs.ArtifactID + "/" + gs.Version + "/"
	
	// Додаємо filename
	if filenameOverride != "" {
		path += filenameOverride
	} else {
		path += gs.GetFileName()
	}
	
	return path
}

// SetClassifier встановлює classifier (для нативних бібліотек).
// Портовано з Prism: GradleSpecifier::setClassifier()
func (gs *GradleSpecifier) SetClassifier(classifier string) {
	gs.Classifier = classifier
}

// GetArtifactBase повертає базову назву без версії та classifier.
// Використовується для дедуплікації бібліотек.
//
// Формат: group:artifact
func (gs *GradleSpecifier) GetArtifactBase() string {
	if !gs.Valid {
		return gs.RawValue
	}
	return gs.GroupID + ":" + gs.ArtifactID
}

// MatchesBase перевіряє чи інший specifier має ту саму group:artifact.
// Використовується для дедуплікації (порівняння версій).
func (gs *GradleSpecifier) MatchesBase(other *GradleSpecifier) bool {
	if !gs.Valid || !other.Valid {
		return false
	}
	return gs.GroupID == other.GroupID && gs.ArtifactID == other.ArtifactID
}

// CompareVersion порівнює версії (просте порівняння рядків).
// Повертає:
//   -1 якщо gs.Version < other.Version
//    0 якщо gs.Version == other.Version
//    1 якщо gs.Version > other.Version
//
// ПРИМІТКА: Це спрощене порівняння (як у старому Prism коді).
// Для повного семантичного версіонування потрібна складніша логіка,
// але Prism також використовує просте порівняння рядків у більшості випадків.
func (gs *GradleSpecifier) CompareVersion(other *GradleSpecifier) int {
	if gs.Version < other.Version {
		return -1
	}
	if gs.Version > other.Version {
		return 1
	}
	return 0
}
