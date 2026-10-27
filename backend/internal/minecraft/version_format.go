package minecraft

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseVersionJSON парсить version.json файл у VersionFile.
// Портовано з Prism Launcher: MojangVersionFormat::versionFileFromJson()
//
// Логіка (ТОЧНО як у Prism):
//   1. Парсинг базових властивостей
//   2. Парсинг бібліотек (з розділенням на звичайні/нативні)
//   3. Парсинг аргументів (legacy та новий формат)
//   4. Парсинг Java вимог
//   5. Парсинг downloads та assetIndex
func ParseVersionJSON(data []byte) (*VersionFile, error) {
	vf := NewVersionFile()
	
	// Парсимо у map для гнучкості
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	
	// Базові властивості
	if err := readVersionProperties(raw, vf); err != nil {
		return nil, err
	}
	
	// Бібліотеки
	if libs, ok := raw["libraries"].([]interface{}); ok {
		for i, libData := range libs {
			libObj, ok := libData.(map[string]interface{})
			if !ok {
				continue
			}
			
			lib, err := parseLibrary(libObj)
			if err != nil {
				// Логуємо помилку але продовжуємо (як у Prism)
				fmt.Printf("Warning: failed to parse library %d: %v\n", i, err)
				continue
			}
			
			vf.AddLibrary(lib)
		}
	}
	
	return vf, nil
}

// readVersionProperties читає базові властивості версії.
// Портовано з Prism: MojangVersionFormat::readVersionProperties()
func readVersionProperties(in map[string]interface{}, out *VersionFile) error {
	// ID (обов'язкове)
	if id, ok := in["id"].(string); ok {
		out.ID = id
	}
	
	// Type
	if vtype, ok := in["type"].(string); ok {
		out.Type = vtype
	}
	
	// MainClass
	if mainClass, ok := in["mainClass"].(string); ok {
		out.MainClass = mainClass
	}
	
	// AppletClass (legacy)
	if appletClass, ok := in["appletClass"].(string); ok {
		out.AppletClass = appletClass
	}
	
	// InheritsFrom
	if inheritsFrom, ok := in["inheritsFrom"].(string); ok {
		out.InheritsFrom = inheritsFrom
	}
	
	// Jar
	if jar, ok := in["jar"].(string); ok {
		out.Jar = jar
	}
	
	// Assets
	if assets, ok := in["assets"].(string); ok {
		out.Assets = assets
	}
	
	// MinimumLauncherVersion
	if minVer, ok := in["minimumLauncherVersion"].(float64); ok {
		out.MinimumLauncherVersion = int(minVer)
		
		// Перевірка (як у Prism: має бути >= 18)
		if out.MinimumLauncherVersion > 21 {
			return fmt.Errorf("minimumLauncherVersion (%d) > 21: unsupported", out.MinimumLauncherVersion)
		}
	}
	
	// MinecraftArguments (legacy формат, string)
	if mcArgs, ok := in["minecraftArguments"].(string); ok {
		out.MinecraftArguments = mcArgs
	}
	
	// Arguments (новий формат, MC >= 1.13)
	if args, ok := in["arguments"].(map[string]interface{}); ok {
		out.Arguments = &ArgumentsSection{}
		
		if game, ok := args["game"].([]interface{}); ok {
			out.Arguments.Game = game
		}
		if jvm, ok := args["jvm"].([]interface{}); ok {
			out.Arguments.JVM = jvm
		}
	}
	
	// JavaVersion
	if javaVer, ok := in["javaVersion"].(map[string]interface{}); ok {
		out.JavaVersion = &JavaVersionInfo{}
		
		if component, ok := javaVer["component"].(string); ok {
			out.JavaVersion.Component = component
		}
		if majorVer, ok := javaVer["majorVersion"].(float64); ok {
			out.JavaVersion.MajorVersion = int(majorVer)
		}
	}
	
	// CompatibleJavaMajors (Prism extended)
	if javaMajors, ok := in["compatibleJavaMajors"].([]interface{}); ok {
		for _, maj := range javaMajors {
			if majorNum, ok := maj.(float64); ok {
				out.CompatibleJavaMajors = append(out.CompatibleJavaMajors, int(majorNum))
			}
		}
	}
	
	// AssetIndex
	if assetIdx, ok := in["assetIndex"].(map[string]interface{}); ok {
		out.AssetIndex = parseAssetIndex(assetIdx)
	}
	
	// Downloads
	if downloads, ok := in["downloads"].(map[string]interface{}); ok {
		out.Downloads = make(map[string]*MojangDownloadInfo)
		for key, dlData := range downloads {
			if dlObj, ok := dlData.(map[string]interface{}); ok {
				out.Downloads[key] = parseDownloadInfo(dlObj)
			}
		}
	}
	
	return nil
}

// parseLibrary парсить одну бібліотеку з JSON.
// Портовано з Prism: MojangVersionFormat::libraryFromJson()
func parseLibrary(in map[string]interface{}) (*Library, error) {
	// Name (обов'язкове)
	name, ok := in["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("library missing 'name' field")
	}
	
	lib := NewLibrary(name)
	
	// URL (repository)
	if url, ok := in["url"].(string); ok {
		lib.RepositoryURL = url
	}
	
	// Downloads (Mojang формат)
	if downloads, ok := in["downloads"].(map[string]interface{}); ok {
		lib.MojangDownloads = parseLibraryDownloads(downloads)
	}
	
	// Natives
	if natives, ok := in["natives"].(map[string]interface{}); ok {
		lib.NativeClassifiers = make(map[string]string)
		for os, classifier := range natives {
			if classStr, ok := classifier.(string); ok {
				lib.NativeClassifiers[os] = classStr
			}
		}
	}
	
	// Extract
	if extract, ok := in["extract"].(map[string]interface{}); ok {
		if exclude, ok := extract["exclude"].([]interface{}); ok {
			lib.HasExcludes = true
			for _, ex := range exclude {
				if exStr, ok := ex.(string); ok {
					lib.ExtractExcludes = append(lib.ExtractExcludes, exStr)
				}
			}
		}
	}
	
	// Rules
	if rules, ok := in["rules"].([]interface{}); ok {
		for _, ruleData := range rules {
			if ruleObj, ok := ruleData.(map[string]interface{}); ok {
				rule := parseRule(ruleObj)
				lib.Rules = append(lib.Rules, rule)
			}
		}
	}
	
	return lib, nil
}

// parseLibraryDownloads парсить downloads секцію бібліотеки.
func parseLibraryDownloads(in map[string]interface{}) *MojangLibraryDownloadInfo {
	info := &MojangLibraryDownloadInfo{
		Classifiers: make(map[string]*MojangDownloadInfo),
	}
	
	// Artifact (головний файл)
	if artifact, ok := in["artifact"].(map[string]interface{}); ok {
		info.Artifact = parseDownloadInfo(artifact)
	}
	
	// Classifiers (natives)
	if classifiers, ok := in["classifiers"].(map[string]interface{}); ok {
		for key, clData := range classifiers {
			if clObj, ok := clData.(map[string]interface{}); ok {
				info.Classifiers[key] = parseDownloadInfo(clObj)
			}
		}
	}
	
	return info
}

// parseDownloadInfo парсить інформацію про завантаження.
func parseDownloadInfo(in map[string]interface{}) *MojangDownloadInfo {
	info := &MojangDownloadInfo{}
	
	if url, ok := in["url"].(string); ok {
		info.URL = url
	}
	if sha1, ok := in["sha1"].(string); ok {
		info.SHA1 = sha1
	}
	if size, ok := in["size"].(float64); ok {
		info.Size = int64(size)
	}
	if path, ok := in["path"].(string); ok {
		info.Path = path
	}
	
	return info
}

// parseAssetIndex парсить assetIndex.
func parseAssetIndex(in map[string]interface{}) *AssetIndexInfo {
	info := &AssetIndexInfo{}
	
	if id, ok := in["id"].(string); ok {
		info.ID = id
	}
	if sha1, ok := in["sha1"].(string); ok {
		info.SHA1 = sha1
	}
	if size, ok := in["size"].(float64); ok {
		info.Size = int64(size)
	}
	if totalSize, ok := in["totalSize"].(float64); ok {
		info.TotalSize = int64(totalSize)
	}
	if url, ok := in["url"].(string); ok {
		info.URL = url
	}
	
	return info
}

// parseRule парсить правило активації.
func parseRule(in map[string]interface{}) Rule {
	rule := Rule{}
	
	if action, ok := in["action"].(string); ok {
		rule.Action = action
	}
	
	if os, ok := in["os"].(map[string]interface{}); ok {
		rule.OS = &RuleOS{}
		
		if name, ok := os["name"].(string); ok {
			rule.OS.Name = name
		}
		if version, ok := os["version"].(string); ok {
			rule.OS.Version = version
		}
		if arch, ok := os["arch"].(string); ok {
			rule.OS.Arch = arch
		}
	}
	
	if features, ok := in["features"].(map[string]interface{}); ok {
		rule.Features = make(map[string]bool)
		for key, val := range features {
			if boolVal, ok := val.(bool); ok {
				rule.Features[key] = boolVal
			}
		}
	}
	
	return rule
}

// ResolveLibraryArtifact заповнює download info для бібліотек без Mojang downloads.
// Портовано з installer.go: resolveLibraryArtifact()
//
// Для Fabric/Quilt бібліотек які мають лише name + url (без downloads),
// генерує Path та URL через gradlePath.
func ResolveLibraryArtifact(lib *Library) bool {
	if lib == nil || !lib.Name.Valid {
		return false
	}
	
	// Якщо вже є downloads.artifact - нічого робити
	if lib.MojangDownloads != nil && lib.MojangDownloads.Artifact != nil && 
	   lib.MojangDownloads.Artifact.URL != "" {
		return true
	}
	
	// Генеруємо path з gradle coordinates
	path := lib.Name.ToPath("")
	if path == "" || path == lib.Name.Serialize() {
		return false
	}
	
	// Формуємо URL
	baseURL := lib.RepositoryURL
	if baseURL == "" {
		baseURL = "https://repo1.maven.org/maven2" // Maven Central fallback
	}
	
	// Створюємо downloads структуру
	if lib.MojangDownloads == nil {
		lib.MojangDownloads = &MojangLibraryDownloadInfo{
			Classifiers: make(map[string]*MojangDownloadInfo),
		}
	}
	
	lib.MojangDownloads.Artifact = &MojangDownloadInfo{
		Path: path,
		URL:  strings.TrimSuffix(baseURL, "/") + "/" + path,
	}
	
	return true
}
