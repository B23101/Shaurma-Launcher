// Package wardrobe реалізує сторінку "Гардероб": збереження пресетів
// скінів/плащів локально та інтеграцію з Mojang API (застосування скіна
// на ліцензійний акаунт, керування плащами, пошук скіна іншого гравця
// за ніком).
//
// Перенесено з JavaFX-версії (WardrobeScreen.java, MojangSkinService.java,
// SkinPresetStore.java, SkinPreset.java) — формат presets.json свідомо
// сумісний зі старим лаунчером.
package wardrobe

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-wails/internal/model"
)

// TokenRefresher — колбек у App, що освіжає Microsoft/Minecraft токен
// акаунта за потреби (уникає циклічного імпорту wardrobe↔auth↔config).
// Повертає актуальний model.Account з дійсним AccessToken.
type TokenRefresher func(accountID string) (model.Account, error)

type Service struct {
	store    *Store
	mojang   *mojangClient
	skinsDir string

	refresh TokenRefresher
}

func NewService(skinsDir string, refresh TokenRefresher) *Service {
	return &Service{
		store:    NewStore(skinsDir),
		mojang:   newMojangClient(),
		skinsDir: skinsDir,
		refresh:  refresh,
	}
}

// ── Пресети (локальне сховище, окреме для кожного акаунта) ──────────────

func (s *Service) ListPresets(accountID string) []model.SkinPreset { return s.store.List(accountID) }

func (s *Service) DeletePreset(accountID, id string) error { return s.store.Delete(accountID, id) }

// SaveLocalSkin створює/оновлює пресет із PNG-байтів скіна, завантажених
// користувачем вручну (аналог AddSkinCardComponent + drag&drop у Java).
func (s *Service) SaveLocalSkin(accountID, name string, skinPNG []byte, slim bool) (model.SkinPreset, error) {
	skinPath, err := s.writeTexture("skin", skinPNG)
	if err != nil {
		return model.SkinPreset{}, err
	}
	return s.store.Save(accountID, model.SkinPreset{
		Name:     name,
		SkinPath: skinPath,
		SlimArms: slim,
		Source:   "local",
	})
}

// UpdatePreset оновлює існуючий пресет: назва, тип рук і плащ (кнопка
// «Зберегти» у редакторі пресету). Якщо capeURL порожній — плащ з пресету
// знімається; інакше плащ завантажується з Mojang-CDN у локальний кеш
// (capePath) і прив'язується до пресету, щоб картка/3D одразу показали
// новий вигляд.
func (s *Service) UpdatePreset(accountID, presetID, name string, slim bool, capeURL string) (model.SkinPreset, error) {
	list := s.store.List(accountID)
	var preset *model.SkinPreset
	for i := range list {
		if list[i].ID == presetID {
			preset = &list[i]
			break
		}
	}
	if preset == nil {
		return model.SkinPreset{}, errors.New("wardrobe: preset not found")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = preset.Name
	}
	preset.Name = name
	preset.SlimArms = slim
	if capeURL == "" {
		preset.CapePath = ""
		preset.CapeURL = ""
	} else if capeURL != preset.CapeURL {
		// Плащ не змінився — не качаємо його знову з мережі (БЕЗ зайвого
		// round-trip при простому перейменуванні пресету).
		capeBytes, err := s.mojang.DownloadTexture(capeURL)
		if err != nil {
			return model.SkinPreset{}, err
		}
		capePath, err := s.writeTexture("cape", capeBytes)
		if err != nil {
			return model.SkinPreset{}, err
		}
		preset.CapePath = capePath
		preset.CapeURL = capeURL
	}

	return s.store.Save(accountID, *preset)
}

// ── "Скопіювати скін гравця" (нова фіча) ─────────────────────────────────

// LookupPlayer шукає гравця за ніком на Mojang і повертає превʼю
// (нік, UUID, classic/slim, наявність плаща) БЕЗ створення пресету —
// фронтенд викликає це під час набору ніку в модалці (з debounce).
func (s *Service) LookupPlayer(nickname string) (*model.PlayerProfile, error) {
	if len(nickname) < 3 || len(nickname) > 16 {
		return nil, errors.New("wardrobe: invalid nickname length")
	}
	return s.mojang.LookupByNickname(nickname)
}

// CopyPlayerSkin завершує фічу "Скопіювати скін гравця": завантажує PNG
// скіна (і опційно плаща) знайденого гравця, зберігає локально і
// створює новий SkinPreset для вказаного акаунта.
func (s *Service) CopyPlayerSkin(accountID string, profile model.PlayerProfile, includeCape bool) (model.SkinPreset, error) {
	skinBytes, err := s.mojang.DownloadTexture(profile.SkinURL)
	if err != nil {
		return model.SkinPreset{}, err
	}
	skinPath, err := s.writeTexture("skin", skinBytes)
	if err != nil {
		return model.SkinPreset{}, err
	}

	preset := model.SkinPreset{
		Name:       profile.Nickname,
		SkinPath:   skinPath,
		SlimArms:   profile.SlimArms,
		Source:     "mojang-copy",
		SourceNick: profile.Nickname,
	}

	if includeCape && profile.CapeURL != "" {
		capeBytes, err := s.mojang.DownloadTexture(profile.CapeURL)
		if err == nil { // плащ не критичний — при помилці просто зберігаємо без нього
			capePath, err := s.writeTexture("cape", capeBytes)
			if err == nil {
				preset.CapePath = capePath
				preset.CapeURL = profile.CapeURL
			}
		}
	}

	return s.store.Save(accountID, preset)
}

// ── Застосування на власний акаунт ───────────────────────────────────────

// ApplyPreset вивантажує скін пресету на ліцензійний акаунт користувача.
// accountID передається з фронтенду (активний акаунт); токен освіжається
// через колбек TokenRefresher так само, як перед запуском гри в app.go.
func (s *Service) ApplyPreset(accountID string, presetID string) error {
	var preset *model.SkinPreset
	for _, p := range s.store.List(accountID) {
		if p.ID == presetID {
			pp := p
			preset = &pp
			break
		}
	}
	if preset == nil {
		return errors.New("wardrobe: preset not found")
	}

	skinBytes, err := os.ReadFile(preset.SkinPath)
	if err != nil {
		return err
	}

	account, err := s.refresh(accountID)
	if err != nil {
		return err
	}
	if !account.IsLicensed || account.AccessToken == "" {
		return errors.New("wardrobe: apply skin requires a licensed Microsoft account")
	}

	if err := s.mojang.UploadSkin(account.AccessToken, skinBytes, preset.SlimArms); err != nil {
		return err
	}

	if preset.CapePath != "" {
		// Плащ можна активувати лише якщо він вже виданий акаунту Mojang —
		// звірка відбувається на боці FetchOwnedCapes/SetActiveCape нижче,
		// тут просто пробуємо застосувати найближчий відповідний плащ.
		capes, err := s.mojang.FetchOwnedCapes(account.AccessToken)
		if err == nil {
			for _, c := range capes {
				if c.URL == preset.CapeURL {
					_ = s.mojang.SetActiveCape(account.AccessToken, c.ID)
					break
				}
			}
		}
	}

	return nil
}

// ActiveMatchingPresetID шукає серед збережених пресетів той, чия текстура
// скіна (і плаща, якщо є) БАЙТ-В-БАЙТ збігається з тим, що зараз реально
// стоїть на акаунті гравця на Mojang. Використовується, щоб автоматично
// підсвітити у гардеробі пресет, який вже фактично застосований —
// порівняння за вмістом файлу, а не за назвою/ID, тому працює навіть
// якщо той самий скін завантажили як два різні пресети з різними іменами.
func (s *Service) ActiveMatchingPresetID(accountID string) (string, error) {
	account, err := s.refresh(accountID)
	if err != nil {
		return "", err
	}
	if !account.IsLicensed || account.AccessToken == "" {
		return "", nil
	}

	skinURL, capeURL, _, err := s.mojang.FetchOwnActiveTextures(account.AccessToken)
	if err != nil {
		return "", err
	}
	if skinURL == "" {
		return "", nil
	}

	remoteSkin, err := s.mojang.DownloadTexture(skinURL)
	if err != nil {
		return "", err
	}
	remoteSkinHash := sha256.Sum256(remoteSkin)

	var remoteCapeHash [32]byte
	hasRemoteCape := false
	if capeURL != "" {
		remoteCape, err := s.mojang.DownloadTexture(capeURL)
		if err == nil {
			remoteCapeHash = sha256.Sum256(remoteCape)
			hasRemoteCape = true
		}
	}

	for _, p := range s.store.List(accountID) {
		localSkin, err := os.ReadFile(p.SkinPath)
		if err != nil {
			continue
		}
		if sha256.Sum256(localSkin) != remoteSkinHash {
			continue
		}

		presetHasCape := p.CapePath != ""
		if presetHasCape != hasRemoteCape {
			continue
		}
		if presetHasCape {
			localCape, err := os.ReadFile(p.CapePath)
			if err != nil || sha256.Sum256(localCape) != remoteCapeHash {
				continue
			}
		}

		return p.ID, nil
	}

	return "", nil
}

func (s *Service) FetchOwnedCapes(accountID string) ([]model.CapeInfo, error) {
	account, err := s.refresh(accountID)
	if err != nil {
		return nil, err
	}
	return s.mojang.FetchOwnedCapes(account.AccessToken)
}

func (s *Service) SetActiveCape(accountID, capeID string) error {
	account, err := s.refresh(accountID)
	if err != nil {
		return err
	}
	return s.mojang.SetActiveCape(account.AccessToken, capeID)
}

// ── Поточний вигляд акаунта (як у старому лаунчері) ─────────────────────

// ActiveLook повертає те, що зараз реально стоїть на Mojang-акаунті
// (активний скін + активний плащ) як data URL для прев'ю в гардеробі,
// коли жоден збережений пресет не збігається з поточним виглядом акаунта.
// Повертає nil, якщо акаунт не ліцензійний або в нього немає власного скіна.
func (s *Service) ActiveLook(accountID string) (*model.ActiveLook, error) {
	account, err := s.refresh(accountID)
	if err != nil {
		return nil, err
	}
	if !account.IsLicensed || account.AccessToken == "" {
		return nil, nil
	}

	skinURL, capeURL, slim, err := s.mojang.FetchOwnActiveTextures(account.AccessToken)
	if err != nil {
		return nil, err
	}
	if skinURL == "" {
		return nil, nil
	}

	skinBytes, err := s.mojang.DownloadTexture(skinURL)
	if err != nil {
		return nil, err
	}
	look := &model.ActiveLook{
		SkinDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(skinBytes),
		SlimArms:    slim,
	}
	if capeURL != "" {
		if capeBytes, err := s.mojang.DownloadTexture(capeURL); err == nil {
			look.CapeDataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(capeBytes)
		}
	}
	return look, nil
}

// SaveActiveLookPreset створює пресет з поточного вигляду власного акаунта
// (кнопка "Редагувати скін", коли поточний скін/плащ не мають збереженого
// пресету). Те саме, що CopyPlayerSkin, але з акаунта самого користувача.
func (s *Service) SaveActiveLookPreset(accountID, name string) (model.SkinPreset, error) {
	account, err := s.refresh(accountID)
	if err != nil {
		return model.SkinPreset{}, err
	}
	if !account.IsLicensed || account.AccessToken == "" {
		return model.SkinPreset{}, errors.New("wardrobe: creating a preset requires a licensed Microsoft account")
	}

	skinURL, capeURL, slim, err := s.mojang.FetchOwnActiveTextures(account.AccessToken)
	if err != nil {
		return model.SkinPreset{}, err
	}
	if skinURL == "" {
		return model.SkinPreset{}, errors.New("wardrobe: account has no custom skin")
	}

	skinBytes, err := s.mojang.DownloadTexture(skinURL)
	if err != nil {
		return model.SkinPreset{}, err
	}
	skinPath, err := s.writeTexture("skin", skinBytes)
	if err != nil {
		return model.SkinPreset{}, err
	}

	if name == "" {
		name = account.Username
	}
	preset := model.SkinPreset{
		Name:       name,
		SkinPath:   skinPath,
		SlimArms:   slim,
		Source:     "mojang-copy",
		SourceNick: account.Username,
	}

	if capeURL != "" {
		if capeBytes, err := s.mojang.DownloadTexture(capeURL); err == nil {
			if capePath, err := s.writeTexture("cape", capeBytes); err == nil {
				preset.CapePath = capePath
				preset.CapeURL = capeURL
			}
		}
	}

	return s.store.Save(accountID, preset)
}

// ── Допоміжне ─────────────────────────────────────────────────────────

// writeTexture зберігає PNG у ~/.shaurma/skins/{sha256}.png — дедуплікація
// за вмістом (як ActiveSkinStore.java у старому лаунчері хешував текстуру).
func (s *Service) writeTexture(kind string, data []byte) (string, error) {
	if err := os.MkdirAll(s.skinsDir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	name := kind + "-" + hex.EncodeToString(sum[:8]) + ".png"
	path := filepath.Join(s.skinsDir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil // вже збережено раніше — не пишемо повторно
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// TextureDataURL читає PNG з диска і повертає data URL для показу у
// фронтенді (аналог avatar.Service.Head, але для повних текстур скіна).
func (s *Service) TextureDataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// CapeDataURL завантажує плащ за Mojang-CDN URL і повертає data URL для
// превʼю на 3D-моделі в редакторі (плащі з акаунта приходять як remote URL,
// без цього методу фронтенд не міг би їх показати через CORS).
func (s *Service) CapeDataURL(capeURL string) (string, error) {
	if capeURL == "" {
		return "", errors.New("wardrobe: empty cape url")
	}
	data, err := s.mojang.DownloadTexture(capeURL)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// RemoteSkinDataURL завантажує довільну текстуру скіна за Mojang-CDN URL
// (skinUrl іншого гравця з PlayerProfile) і повертає data URL. Потрібен,
// бо THREE.Texture не може напряму завантажити зображення з Mojang CDN
// через CORS — фронтенд у модалці "Скопіювати скін гравця" проксує запит
// через бекенд так само, як це вже робиться для плащів (CapeDataURL).
func (s *Service) RemoteSkinDataURL(skinURL string) (string, error) {
	if skinURL == "" {
		return "", errors.New("wardrobe: empty skin url")
	}
	data, err := s.mojang.DownloadTexture(skinURL)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}
