// Package mcoptions — читання та запис options.txt Minecraft (налаштування
// гри «за замовчуванням»). Це порт OptionsFileManager старого Java-лаунчера.
//
// Принцип: лаунчер знає і редагує лише ПІДМНОЖИНУ опцій options.txt
// («видимі» опції — з розділу «За умовчанням» старого лаунчера). Решту
// файла (опції модів тощо) ми не чіпаємо ніколи.
//
// ВАЖЛИВО: цей пакет НЕ зберігає жодного службового стану лаунчера у
// самому options.txt (раніше тут був рядок-маркер "shaurma_pack:true" —
// див. МАРКЕР нижче). "Чи вже застосовувались дефолти цій збірці" —
// питання виключно про ОДНУ конкретну збірку (packID), і відповідь на
// нього живе в instance.json цієї збірки (model.InstanceConfig.
// MinecraftDefaultsApplied, internal/config.Manager), а не у файлі,
// яким володіє гравець. mcoptions лише виконує саму дію запису/читання
// options.txt за командою викликача — рішення "застосовувати чи ні"
// приймає викликач (App.applyMinecraftDefaultsNow/BeforeLaunch).
package mcoptions

// soundCategories — ванільні категорії звуку, які лаунчер показує/редагує.
func soundCategories() []string {
	return []string{
		"master", "music", "record", "weather", "block",
		"hostile", "neutral", "player", "ambient", "voice",
	}
}

// bindKeys — ванільні прив'язки клавіш (ключі без префікса "key_").
func bindKeys() []string {
	return []string{
		"key.attack", "key.use", "key.forward", "key.back",
		"key.left", "key.right", "key.jump", "key.sneak",
		"key.sprint", "key.inventory", "key.swapOffhand", "key.drop",
		"key.chat", "key.command", "key.playerlist", "key.pickItem",
		"key.screenshot", "key.togglePerspective", "key.fullscreen",
		"key.spectatorOutlines", "key.smoothCamera", "key.advancements",
		"key.saveToolbarActivator", "key.loadToolbarActivator",
		"key.hotbar.1", "key.hotbar.2", "key.hotbar.3", "key.hotbar.4",
		"key.hotbar.5", "key.hotbar.6", "key.hotbar.7", "key.hotbar.8",
		"key.hotbar.9",
	}
}

// VisibleKeys повертає всі ключі options.txt, які лаунчер знає і може
// читати/редагувати (розділ «За умовчанням»). Все інше — не чіпаємо.
func VisibleKeys() []string {
	keys := []string{
		"lang", "guiScale",
		"fullscreen", "enableVsync", "maxFps",
		"autoJump", "mouseSensitivity", "invertYMouse",
		"fov", "darkMojangStudiosBackground",
	}
	for _, cat := range soundCategories() {
		keys = append(keys, "soundCategory_"+cat)
	}
	for _, b := range bindKeys() {
		keys = append(keys, "key_"+b)
	}
	return keys
}
