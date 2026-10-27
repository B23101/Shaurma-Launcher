package minecraft

import (
	"strings"
)

// ProcessMinecraftArgs обробляє аргументи Minecraft з підстановкою змінних.
// Портовано з Prism: MinecraftInstance::processMinecraftArgs()
//
// Логіка (ТОЧНО як у Prism):
//   1. Розбиває minecraftArguments на масив
//   2. Додає tweakers (--tweakClass для кожного)
//   3. Підставляє змінні ${variable}
//   4. Додає auth токени (якщо є session)
func ProcessMinecraftArgs(profile *LaunchProfile, tokens map[string]string) []string {
	args := []string{}
	
	// 1. Базові аргументи з профілю
	if profile.MinecraftArguments != "" {
		baseArgs := strings.Fields(profile.MinecraftArguments)
		args = append(args, baseArgs...)
	}
	
	// 2. Додати tweakers
	for _, tweaker := range profile.GetTweakers() {
		args = append(args, "--tweakClass", tweaker)
	}
	
	// 3. Підстановка змінних
	for i := range args {
		args[i] = replaceTokensIn(args[i], tokens)
	}
	
	return args
}

// MakeProfileVarMapping створює мапу змінних для підстановки.
// Портовано з Prism: MinecraftInstance::makeProfileVarMapping()
//
// Змінні (ТОЧНО як у Prism):
//   ${profile_name} - назва профілю
//   ${version_name} - версія Minecraft
//   ${version_type} - тип версії
//   ${game_directory} - .minecraft директорія
//   ${game_assets} - assets директорія
//   ${assets_root} - базова assets директорія
//   ${assets_index_name} - ID asset index
//   ${library_directory} - libraries директорія
func MakeProfileVarMapping(
	profile *LaunchProfile,
	profileName string,
	gameDir string,
	assetsDir string,
	librariesDir string,
) map[string]string {
	result := make(map[string]string)
	
	result["profile_name"] = profileName
	result["version_name"] = profile.MinecraftVersion
	result["version_type"] = profile.MinecraftVersionType
	result["game_directory"] = gameDir
	result["assets_root"] = assetsDir
	result["library_directory"] = librariesDir
	
	// Assets
	if profile.MinecraftAssets != nil {
		result["assets_index_name"] = profile.MinecraftAssets.ID
		result["game_assets"] = assetsDir + "/virtual/" + profile.MinecraftAssets.ID
	} else {
		result["assets_index_name"] = "legacy"
		result["game_assets"] = assetsDir
	}
	
	return result
}

// MakeAuthTokens створює мапу auth токенів.
// Портовано з Prism логіки у processMinecraftArgs()
func MakeAuthTokens(
	playerName string,
	uuid string,
	accessToken string,
	userType string,
) map[string]string {
	tokens := make(map[string]string)
	
	tokens["auth_player_name"] = playerName
	tokens["auth_uuid"] = uuid
	tokens["auth_access_token"] = accessToken
	tokens["user_type"] = userType
	
	// Legacy токени (для старих версій MC)
	tokens["auth_session"] = "token:" + accessToken + ":" + uuid
	tokens["user_properties"] = "{}"
	
	return tokens
}

// replaceTokensIn замінює ${variable} на значення з tokens.
// Портовано з Prism: replaceTokensIn()
//
// Логіка (ТОЧНО як у Prism):
//   1. Шукає ${...}
//   2. Витягує ім'я змінної
//   3. Замінює на значення з tokens
//   4. Якщо немає в tokens → залишає як є
func replaceTokensIn(input string, tokens map[string]string) string {
	result := input
	
	// Простий парсинг ${...}
	for key, value := range tokens {
		placeholder := "${" + key + "}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	
	return result
}

// ProcessJvmArgs обробляє JVM аргументи.
// Портовано з Prism: MinecraftInstance::javaArguments()
//
// Логіка (ТОЧНО як у Prism):
//   1. -Duser.language=en
//   2. Додаткові аргументи (extraArguments)
//   3. Пам'ять (-Xms, -Xmx)
//   4. PermGen (для старої Java)
//   5. Java 9+ модулі (--add-opens)
func ProcessJvmArgs(
	profile *LaunchProfile,
	minRAM int,
	maxRAM int,
	javaVersion int,
	extraArgs []string,
) []string {
	args := []string{}
	
	// 1. Мова
	args = append(args, "-Duser.language=en")
	
	// 2. Додаткові аргументи
	args = append(args, extraArgs...)
	
	// 3. Додаткові JVM аргументи з профілю
	args = append(args, profile.GetAddnJvmArguments()...)
	
	// 4. Пам'ять
	if minRAM > 0 {
		args = append(args, "-Xms"+intToStr(minRAM)+"m")
	}
	if maxRAM > 0 {
		args = append(args, "-Xmx"+intToStr(maxRAM)+"m")
	}
	
	// 5. PermGen для старої Java (< 8)
	if javaVersion > 0 && javaVersion < 8 {
		args = append(args, "-XX:PermSize=128m")
	}
	
	// 6. Java 9+ модулі для онлайн фіксів
	if javaVersion >= 9 {
		args = append(args, "--add-opens", "java.base/java.net=ALL-UNNAMED")
	}
	
	return args
}

// AddJavaAgents додає Java агенти до JVM аргументів.
// Формат: -javaagent:path[=argument]
func AddJavaAgents(args []string, agents []JavaAgent, basePath string) []string {
	ctx := NewRuntimeContext()
	
	for _, agent := range agents {
		if agent.Library != nil {
			agentPath := agent.Library.ActualPath(ctx, basePath)
			agentArg := "-javaagent:" + agentPath
			if agent.Argument != "" {
				agentArg += "=" + agent.Argument
			}
			args = append(args, agentArg)
		}
	}
	
	return args
}

// AddNativeLibraryPath додає -Djava.library.path для нативних бібліотек.
// Портовано з Prism логіки.
func AddNativeLibraryPath(args []string, nativesDir string) []string {
	return append(args, "-Djava.library.path="+nativesDir)
}

// MergeTokens об'єднує кілька мап токенів в одну.
func MergeTokens(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// ExtractNatives розпаковує нативні бібліотеки у targetDir.
// Портовано логіку з Prism (але спрощено, бо розпакування
// вже реалізоване в installer.go).
func ExtractNatives(nativeJars []string, targetDir string, excludes []string) error {
	// TODO: реалізувати розпакування з урахуванням excludes
	// Наразі це робиться в ensureNatives() у installer.go
	return nil
}

// intToStr коректна реалізація
func intToStr(i int) string {
	// Проста конвертація
	if i == 0 {
		return "0"
	}
	
	negative := i < 0
	if negative {
		i = -i
	}
	
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + (i % 10))}, digits...)
		i /= 10
	}
	
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	
	return string(digits)
}
