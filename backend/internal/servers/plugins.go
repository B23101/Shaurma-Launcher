// plugins.go — менеджер плагінів Bukkit/Paper/Purpur/Velocity (plugins/ папка).
package servers

import (
	"os"
	"path/filepath"
)

// ServerPluginInfo — інформація про плагін сервера.
type ServerPluginInfo = ServerModInfo // той самий формат що й моди

// ListServerPlugins повертає список плагінів з папки plugins/.
func ListServerPlugins(serverDir string) []ServerPluginInfo {
	pluginsDir := serverDir + "/plugins"
	return listJarFiles(pluginsDir)
}

// ToggleServerPlugin вмикає/вимикає плагін (plugins/ папка).
// Використовує той самий toggleServerFile, що й моди: стан береться з
// реального імені на диску, дубльовані суфікси .disabled лікуються
// (див. коментар у mods.go — плагіни мали ту саму помилку).
func ToggleServerPlugin(serverDir, fileName string) error {
	return toggleServerFile(filepath.Join(serverDir, "plugins"), fileName)
}

// DeleteServerPlugin видаляє плагін з plugins/ (усі варіанти імені).
func DeleteServerPlugin(serverDir, fileName string) error {
	return deleteServerFile(filepath.Join(serverDir, "plugins"), fileName)
}

// HasPluginsDir перевіряє чи є папка plugins/.
func HasPluginsDir(serverDir string) bool {
	info, err := os.Stat(serverDir + "/plugins")
	return err == nil && info.IsDir()
}
