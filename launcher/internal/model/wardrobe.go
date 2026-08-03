package model

// SkinPreset — збережений пресет гардеробу (скін + опційний плащ).
// Формат полів свідомо сумісний зі старим JavaFX-лаунчером
// (SkinPreset.java: id, name, skinPath, slimArms, capePath/capeUrl), щоб
// файл presets.json можна було перенести без міграції даних.
type SkinPreset struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SkinPath  string `json:"skinPath"`            // шлях до PNG 64x64/64x32 у ~/.shaurma/skins/
	SlimArms  bool   `json:"slimArms"`             // classic (4px) / slim (3px)
	CapePath  string `json:"capePath,omitempty"`   // локальний кеш плаща (порожньо — без плаща)
	CapeURL   string `json:"capeUrl,omitempty"`    // джерело плаща на Mojang (для оновлення кешу)
	Source    string `json:"source"`               // "local" | "mojang-copy" | "default"
	SourceNick string `json:"sourceNick,omitempty"` // нік гравця, якщо Source == "mojang-copy"
	CreatedAt int64  `json:"createdAt"`
}

// CapeInfo — плащ, доступний акаунту гравця через Mojang
// (аналог CapeInfo(id, url, alias, active) зі старого лаунчера).
type CapeInfo struct {
	ID     string `json:"id"`
	Alias  string `json:"alias"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// PlayerProfile — результат пошуку гравця за ніком (фіча "Скопіювати
// скін гравця"): api.mojang.com/users/profiles/minecraft/{nick} →
// sessionserver.mojang.com/session/minecraft/profile/{uuid}.
type PlayerProfile struct {
	Nickname string `json:"nickname"`
	UUID     string `json:"uuid"`
	SlimArms bool   `json:"slimArms"`
	SkinURL  string `json:"skinUrl"`
	CapeURL  string `json:"capeUrl,omitempty"` // порожньо — гравець без активного плаща
}

// ActiveLook — поточний вигляд ліцензійного акаунта на Mojang (активний
// скін + активний плащ) у вигляді data URL для прев'ю у гардеробі. Віддається,
// коли жоден збережений пресет не збігається з тим, що реально стоїть на
// акаунті (тобто скін не був застосований через гардероб).
type ActiveLook struct {
	SkinDataURL string `json:"skinDataUrl"`
	CapeDataURL string `json:"capeDataUrl,omitempty"` // порожньо — без активного плаща
	SlimArms    bool   `json:"slimArms"`
}
