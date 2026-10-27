package main

// ShaurmaAccountInfo — що UI знає про акаунт Шаурми АКТИВНОГО акаунта лаунчера. Тип спільний для
// обох редакцій (у base-збірці методи — заглушки), щоб фронтенд мав один контракт.
type ShaurmaAccountInfo struct {
	HasAccount        bool   `json:"hasAccount"`
	AccountID         string `json:"accountId,omitempty"` // за ним додають у друзі
	Nickname          string `json:"nickname,omitempty"`  // лише відображення, не унікальний
	LauncherAccountID string `json:"launcherAccountId,omitempty"`
	LauncherNick      string `json:"launcherNick,omitempty"` // підказка ніка для реєстрації
	IsLicensed        bool   `json:"isLicensed"`             // ліцензійний АКАУНТ ЛАУНЧЕРА (Microsoft)
	// Type — тип акаунта ШАУРМИ: "licensed" | "unlicensed" (порожній, якщо акаунта немає).
	Type      string `json:"type,omitempty"`
	UUID      string `json:"uuid,omitempty"` // UUID профілю (Mojang для licensed, офлайн для unlicensed)
	CreatedAt int64  `json:"createdAt,omitempty"`
	// NickLocked — нік береться лише з Mojang (ліцензійний тип): міняти в лаунчері не можна.
	NickLocked bool `json:"nickLocked"`
	// CanUpgrade — акаунт Шаурми неліцензійний, але акаунт лаунчера вже ліцензійний:
	// лаунчер пропонує переоформити акаунт Шаурми як ліцензійний.
	CanUpgrade bool `json:"canUpgrade"`
	// Reason: "" | "no_launcher_account" | "no_hwid"
	Reason string `json:"reason,omitempty"`
}

// ShaurmaProfile — повний профіль акаунта Шаурми для окремої сторінки «Акаунт» у налаштуваннях:
// дані з сервера (тип, UUID, дата створення, ліміт змін ніка) + локальна статистика лаунчера.
// Скін тут НЕ показується — гардероб окремо (скіном керує він).
type ShaurmaProfile struct {
	HasAccount bool   `json:"hasAccount"`
	AccountID  string `json:"accountId,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	Type       string `json:"type,omitempty"` // licensed | unlicensed
	Licensed   bool   `json:"licensed"`
	UUID       string `json:"uuid,omitempty"`
	CreatedAt  int64  `json:"createdAt,omitempty"`

	NickLocked      bool  `json:"nickLocked"`
	NickChangesLeft int   `json:"nickChangesLeft"`
	NickResetAt     int64 `json:"nickResetAt,omitempty"`

	// Handle — публічний @handle для пошуку друзів (без «@»); HandleNextChangeAt — unix мс, коли можна змінити знову (0 — зараз).
	Handle             string `json:"handle,omitempty"`
	HandleNextChangeAt int64  `json:"handleNextChangeAt,omitempty"`

	// Bio — короткий опис у профілі (до 120 символів); Color — колір фону профілю #RRGGBB (порожньо — стандартний).
	Bio   string `json:"bio,omitempty"`
	Color string `json:"color,omitempty"`

	LauncherAccountID string `json:"launcherAccountId,omitempty"`
	LauncherNick      string `json:"launcherNick,omitempty"`
	CanUpgrade        bool   `json:"canUpgrade"`

	// Статистика лаунчера (локальна, показується у профілі).
	GameSeconds int64 `json:"gameSeconds"`
	BuildsCount int   `json:"buildsCount"`
	Friends     int   `json:"friends"`

	// Reason: "" | "no_launcher_account" | "no_hwid" | "no_shaurma_account"
	Reason string `json:"reason,omitempty"`
}

// SignInResult — підсумок кроку входу (Google / Microsoft) для UI.
//
//	Status "signed_in" — готово: лаунчер-акаунт додано й активовано;
//	Status "device_limit" — особу підтверджено, але в акаунта максимум пристроїв: оберіть, який вилучити;
//	Status "need_nick" — особу підтверджено, акаунта Шаурми ще немає, треба обрати нік
//	                     (CompleteSignIn). Для Microsoft сюди ж потрапляє акаунт БЕЗ ліцензії.
type SignInResult struct {
	Status   string `json:"status"`
	Provider string `json:"provider"`           // "google" | "microsoft"
	Nickname string `json:"nickname,omitempty"` // signed_in
	Licensed bool   `json:"licensed"`
	// Existing — акаунт Шаурми цієї особи вже був (відновлено, а не створено).
	Existing bool `json:"existing"`
	// NoLicense — лише при need_nick через Microsoft: "no_license" | "no_xbox".
	NoLicense string `json:"noLicense,omitempty"`
	// Status "device_limit": в акаунта вже максимум пристроїв — оберіть, який вилучити
	// (ReplaceShaurmaDevice). Devices/MaxDevices — для списку вибору.
	Devices    []DeviceChoice `json:"devices,omitempty"`
	MaxDevices int            `json:"maxDevices,omitempty"`
}

// DeviceChoice — пристрій у списку «кого вилучити» при досягнутому ліміті.
type DeviceChoice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ClientName string `json:"clientName"`
	OS         string `json:"os"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Country    string `json:"country"`
}

// NickCheckResult — відповідь «чи можна взяти цей нік».
type NickCheckResult struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"` // invalid | taken | mojang
	Message   string `json:"message,omitempty"`
}
