package shaurmaid

import (
	"context"
	"time"
)

// HWIDGenerator — апаратний відбиток ПК (sha256 hex, 64 символи). Стабільний між
// запусками; MAC-адреси НЕ входять (зміна адаптера/VPN не повинна блокувати акаунт).
type HWIDGenerator interface {
	HWID() (string, error)
}

// Identity — довгостроковий ключ комірки. Приватна частина ніколи не залишає лаунчер.
type Identity struct {
	PublicKey string // base64url, 32 байти (Ed25519)
	Slot      string // випадковий ідентифікатор комірки (base64url)
	private   []byte
}

// CryptoService — криптографія акаунта. E2EE чату (X25519 + ChaCha20-Poly1305) живе в
// internal/social і сюди не змішується: тут лише автентифікація.
type CryptoService interface {
	NewIdentity() (*Identity, error)
	Sign(id *Identity, message string) (string, error)
	SolvePoW(publicKey string, ts int64, bits int) (string, error)
}

// LocalSecureStorage — локальне захищене сховище секретів (OS-кредитне сховище там, де
// воно є). Бекенд/сервер до нього доступу не має.
type LocalSecureStorage interface {
	Put(key string, value []byte) error
	Get(key string) ([]byte, error) // ErrNotFound, якщо немає
	Delete(key string) error
}

// Тип акаунта Шаурми (дзеркало accounts.TypeLicensed/TypeUnlicensed на VPS).
const (
	TypeLicensed   = "licensed"   // нік лише з Mojang, скін з Mojang за UUID
	TypeUnlicensed = "unlicensed" // нік обирає гравець, скін із сервера Шаурми
)

// Cell — публічний вигляд комірки.
type Cell struct {
	AccountID string // унікальний ID: за ним додають у друзі
	Nickname  string // лише відображення, не унікальний
	Type      string // TypeLicensed | TypeUnlicensed
	UUID      string // UUID профілю (32 hex): Mojang для licensed, офлайн для unlicensed
	CreatedAt int64  // коли акаунт створено на цьому ПК, unix ms
}

// Licensed — акаунт ліцензійного типу (нік заблоковано: лише з Mojang).
func (c *Cell) Licensed() bool { return c != nil && c.Type == TypeLicensed }

// RegisterOpts — що лаунчер повідомляє серверу при створенні акаунта.
type RegisterOpts struct {
	Nickname string
	Provider string // "google" | "microsoft"
	Type     string // TypeLicensed | TypeUnlicensed
	// UUID потрібен для licensed (UUID профілю Mojang). Для unlicensed ігнорується:
	// його обчислює сам менеджер з ніка (офлайн-UUID), сервер звіряє.
	UUID string
}

// Pending — «заготовка» комірки до OAuth: ключ уже створено, тож nonce можна покласти ПРЯМО
// у запит входу (один вхід у Microsoft замість двох). Приватний ключ нікуди не передається;
// якщо вхід не завершився — заготовка просто відкидається.
type Pending struct {
	ident *Identity
	nonce string
}

// Nonce — значення nonce для OAuth-запиту (прив'язує id_token до цього ключа).
func (p *Pending) Nonce() string {
	if p == nil {
		return ""
	}
	return p.nonce
}

// Profile — стан акаунта за даними сервера (сторінка профілю).
type Profile struct {
	AccountID       string `json:"accountId"`
	Nickname        string `json:"nickname"`
	Type            string `json:"type"`
	UUID            string `json:"uuid"`
	CreatedAt       int64  `json:"createdAt"`
	NickLocked      bool   `json:"nickLocked"`
	NickChangesLeft int    `json:"nickChangesLeft"`
	NickResetAt     int64  `json:"nickResetAt,omitempty"`
	// Handle — публічний @handle для пошуку друзів; HandleNextChangeAt — коли можна змінити знову (0 — зараз).
	Handle             string `json:"handle,omitempty"`
	HandleNextChangeAt int64  `json:"handleNextChangeAt,omitempty"`
	// Bio / Color — візитка профілю (до 120 символів; #RRGGBB). Порожні — не задано.
	Bio   string `json:"bio,omitempty"`
	Color string `json:"color,omitempty"`
}

// Session — короткоживуча сесія (access-JWT живе в ПАМ'ЯТІ, на диск не пишеться).
type Session struct {
	AccountID   string
	Nickname    string
	AccessToken string
	ExpiresAt   time.Time
	// DeviceID — id пристрою, для якого видано сесію ("" — сервер ще без підтримки пристроїв).
	DeviceID string
}

// IDTokenSource виконує OAuth (лише scope openid) і повертає id_token із заданим nonce.
// Access/refresh-токени провайдера джерело зобов'язане одразу відкинути.
type IDTokenSource func(ctx context.Context, provider, nonce string) (idToken string, err error)

// AuthManager — керування комірками акаунтів Шаурми.
type AuthManager interface {
	Cell(launcherAccountID string) (*Cell, bool)
	// Register — окремий вхід (Google або Microsoft): менеджер сам просить id_token у src.
	Register(ctx context.Context, launcherAccountID string, opts RegisterOpts, src IDTokenSource) (*Cell, error)
	// Prepare + RegisterWithToken — коли id_token уже отримано під час входу в гру (один вхід).
	Prepare() (*Pending, error)
	RegisterWithToken(ctx context.Context, launcherAccountID string, opts RegisterOpts, p *Pending, idToken string) (*Cell, error)
	Session(ctx context.Context, launcherAccountID string) (*Session, error)
	// SetNickname — лише для неліцензійних; ліцензійному → ErrNickLocked.
	SetNickname(ctx context.Context, launcherAccountID, nickname string) error
	// SyncNickname — ліцензійний: підтягнути нік з Mojang (через сервер). Неліцензійному — no-op.
	SyncNickname(ctx context.Context, launcherAccountID string) (string, error)
	// SetHandle — змінити @handle (ліміт: раз на 7 днів; стартовий не рахується).
	SetHandle(ctx context.Context, launcherAccountID, handle string) error
	// SetProfileStyle — зберегти біо й колір фону профілю (обидва поля одразу; "" — очистити).
	// Формат і частоту (раз на кілька секунд) остаточно перевіряє сервер.
	SetProfileStyle(ctx context.Context, launcherAccountID, bio, color string) error
	// Profile — свіжий профіль із сервера.
	Profile(ctx context.Context, launcherAccountID string) (*Profile, error)
	Delete(ctx context.Context, launcherAccountID string) error // віддалено + локально
	Forget(launcherAccountID string) error                      // лише локально
}
