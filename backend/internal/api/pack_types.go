package api

type PackIndexEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	MCVersion     string `json:"mc_version"`
	LoaderType    string `json:"loader_type"`
	LoaderVersion string `json:"loader_version"`
	IconURL       string `json:"icon_url"`
	BackgroundURL string `json:"background_url,omitempty"`
	// MrpackURL @deprecated: схема V2 більше не публікує .mrpack цілим
	// файлом — лишається порожнім, качання йде через ManifestPath.
	MrpackURL string   `json:"mrpack_url,omitempty"`
	UpdatedAt string   `json:"updated_at"`
	Tags      []string `json:"tags,omitempty"`
	// Color — акцентний колір збірки (hex, напр. "#ff8a00"), якщо його
	// віддає index.json (worker). Порожній = використати колір за
	// замовчуванням на фронтенді.
	Color string `json:"color,omitempty"`
	// Version — остання відома версія збірки з сервера (для стану
	// «Оновити»). За замовчуванням worker може віддавати updated_at.
	Version string `json:"version,omitempty"`
	// ServerIP/ManifestPath — поля V2 (packs-index.json): IP сервера
	// збірки і шлях до її повного packs/<id>/manifest.json.
	ServerIP string `json:"server_ip,omitempty"`
	// OfficialServerSync — тримати офіційний сервер (ServerIP) у servers.dat
	// з актуальним IP із CDN при кожному запуску (див. sync.PacksIndexPack).
	OfficialServerSync bool   `json:"official_server_sync,omitempty"`
	ManifestPath       string `json:"manifest_path,omitempty"`
	// LaunchArgs — аргументи запуску збірки (пише Pack Manager, лаунчер
	// додає їх до команди запуску). RecommendedRAMMB — рекомендована RAM
	// (МБ): якщо ефективна RAM нижча — попередження перед запуском.
	LaunchArgs       []string `json:"launch_args,omitempty"`
	RecommendedRAMMB int      `json:"recommended_ram_mb,omitempty"`
	// ── Серверна папка групування (див. sync.PacksIndexPack) ──
	FolderID       string `json:"folder_id,omitempty"`
	FolderName     string `json:"folder_name,omitempty"`
	FolderColor    string `json:"folder_color,omitempty"`
	FolderOrder    int    `json:"folder_order,omitempty"`
	FolderPosition int    `json:"folder_position,omitempty"`
}

type PackManifest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Type           string `json:"type"`
		TargetGameInfo struct {
			Version string   `json:"version"`
			Loaders []string `json:"loaders"`
		} `json:"targetGameInfo"`
	} `json:"version"`
	Files []struct {
		Path   string `json:"path"`
		Hashes struct {
			Sha1 string `json:"sha1"`
		} `json:"hashes"`
		Downloads []string `json:"downloads"`
		FileSize  int64    `json:"fileSize"`
	} `json:"files"`
	Dependencies struct {
		Minecraft    string `json:"minecraft"`
		Forge        string `json:"forge,omitempty"`
		FabricLoader string `json:"fabric-loader,omitempty"`
		QuiltLoader  string `json:"quilt-loader,omitempty"`
	} `json:"dependencies"`
}
