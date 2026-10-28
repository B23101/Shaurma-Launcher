// p2p_api.go — методи App для P2P-модуля ("LAN через інтернет"),
// викликаються з фронтенду. Уся логіка живе в internal/p2p; тут лише тонке
// прокидання + дві перевірки, які належать App-шару: вимикач у налаштуваннях
// (DisableP2P) і ВХІД В АКАУНТ.
//
// ── ЧОМУ P2P ВИМАГАЄ АКАУНТ ─────────────────────────────────────────────
// Потрібен підтверджений акаунт (Google), щоб:
//  1. було кого підписати як власника кімнати в адмінці (ownerName);
//  2. анонімний офлайн-акаунт не міг засмічувати lobby без жодного сліду.
//
// Сама книга застовблених адрес і ліміт claims — це властивість ЛАУНЧЕРА
// (owner-secret, internal/p2p/owner.go), а не окремого акаунта: усі
// акаунти, під якими будь-коли заходили в цьому лаунчері, бачать один і
// той самий спільний список адрес і ділять один і той самий ліміт на
// Worker'і (MAX_ACTIVE_CLAIMS). accountID тут лишається лише як мітка
// "хто саме застовбив/запустив" для адмінки — він більше НЕ звужує, які
// адреси показуються в списку (internal/p2p/store.go).
//
// Тому без входу в Google-акаунт Shaurma P2P-дії відхиляються зі зрозумілим
// поясненням, а не падають у голий 403.
package main

import (
	"fmt"
	"log"
	"time"

	"shaurma-launcher-backend/internal/p2p"
)

// p2pDisabled — чи вимкнув користувач P2P у налаштуваннях.
func (a *App) p2pDisabled() bool {
	return a.cfg.GetSettings().DisableP2P
}

// errP2PDisabled — однакова помилка для всіх P2P-дій, коли фічу вимкнено.
func errP2PDisabled() error {
	return fmt.Errorf("P2P вимкнено в налаштуваннях лаунчера (Налаштування → Основне)")
}

// p2pAccountCheck повертає (accountID, ownerName, помилка). У Shaurma-
// редакції P2P доступний лише з Google-акаунтом Shaurma (див. p2pIdentity):
// Microsoft/Minecraft-акаунт сам по собі доступу не дає, а його токени на
// сервер не передаються.
func (a *App) p2pAccountCheck() (string, string, error) {
	return a.p2pIdentity()
}

// p2pAccountReason — людяна причина, чому P2P недоступний без акаунта (для
// статусу в UI, щоб блок P2P показував пояснення, а не мертві кнопки).
func (a *App) p2pAccountReason() (bool, string) {
	_, _, err := a.p2pAccountCheck()
	if err == nil {
		return true, ""
	}
	return false, err.Error()
}

// GetP2PStatus повертає стан P2P для UI: чи фіча увімкнена в лаунчері, чи
// готовий сервер (Headscale) і чи є підхожий акаунт.
func (a *App) GetP2PStatus() p2p.P2PStatus {
	accountOK, reason := a.p2pAccountReason()
	return a.p2pAPI.GetStatus(a.p2pDisabled(), accountOK, reason)
}

// ── Host (застовбити адресу, почати/зупинити хостинг) ───────────────────

// ClaimP2PAddress застовблює P2P-адресу на основі базової назви. Якщо цей
// акаунт уже має адресу з такою базою — повертається ВОНА (без нового
// claim'у): саме це прибирає потребу займати нову адресу щораза.
func (a *App) ClaimP2PAddress(baseName string, label string) (p2p.ClaimResult, error) {
	if a.p2pDisabled() {
		return p2p.ClaimResult{}, errP2PDisabled()
	}
	accountID, _, err := a.p2pAccountCheck()
	if err != nil {
		return p2p.ClaimResult{}, err
	}
	// tmp1..tmp3 зарезервовані під тимчасові запрошення (вони не зберігаються).
	if label != p2p.TempLabel && p2p.IsTempBase(baseName) {
		return p2p.ClaimResult{}, fmt.Errorf("назва «%s» зарезервована для тимчасових запрошень — вибери іншу", baseName)
	}
	return a.p2pAPI.ClaimP2PAddress(baseName, accountID, label)
}

// ListP2PAddresses повертає всі адреси, зайняті цим акаунтом (щоб UI показав
// їх списком, а гравець просто вибрав потрібну).
func (a *App) ListP2PAddresses() []p2p.ClaimedAddress {
	accountID, _, err := a.p2pAccountCheck()
	if err != nil {
		return nil
	}
	return a.p2pAPI.ListClaimedAddresses(accountID)
}

// purgeTempP2PAddresses — прибирає тимчасові адреси запрошень, що лишились у книзі (аварійний вихід,
// стара версія лаунчера). Викликається на старті й при виході; живих запрошень у цей момент немає.
func (a *App) purgeTempP2PAddresses(budget time.Duration) {
	if a.p2pAPI == nil {
		return
	}
	if n := a.p2pAPI.PurgeTempAddresses(budget); n > 0 {
		log.Printf("p2p: прибрано тимчасових адрес: %d", n)
	}
}

// AssignP2PAddress закріплює одну зі збережених адрес за конкретним сервером
// (label = id сервера). Викликається, коли гравець у списку своїх адрес
// вибрав, яку саме використовувати для ЦЬОГО сервера.
func (a *App) AssignP2PAddress(subdomain string, serverID string) error {
	if a.p2pDisabled() {
		return errP2PDisabled()
	}
	accountID, _, err := a.p2pAccountCheck()
	if err != nil {
		return err
	}
	return a.p2pAPI.AssignClaimedAddress(subdomain, serverID, accountID)
}

// StartP2PHost піднімає хостинг LAN-світу на вказаному порту під уже
// застовбленою адресою.
//
// TCP-порт = minecraftPort (порт сервера). udpPort — порт UDP-тунелю
// (0 = вимкнено), дефолт у UI = порт сервера, можна змінити на будь-який.
func (a *App) StartP2PHost(minecraftPort string, subdomain string, udpPort int) (p2p.HostStatus, error) {
	if a.p2pDisabled() {
		return p2p.HostStatus{Error: errP2PDisabled().Error()}, errP2PDisabled()
	}
	accountID, ownerName, err := a.p2pAccountCheck()
	if err != nil {
		return p2p.HostStatus{Error: err.Error()}, err
	}
	st, err := a.p2pAPI.StartP2PHost(minecraftPort, subdomain, udpPort, accountID, ownerName)
	a.remoteOnP2PChanged() // друзі з доступом бачать нову адресу
	return st, err
}

// StopP2PHost зупиняє конкретну хост-сесію за її subdomain (адреса
// лишається застовбленою, і в lobby, і в локальній книзі).
func (a *App) StopP2PHost(subdomain string) error {
	// Зупинка НЕ заблокована ні вимикачем, ні акаунтом: якщо сесію підняли
	// раніше, користувач все одно має мати змогу її коректно закрити.
	err := a.p2pAPI.StopP2PHost(subdomain)
	a.remoteOnP2PChanged()
	return err
}

// GetP2PHostStatus повертає стан хостингу для конкретної адреси.
func (a *App) GetP2PHostStatus(subdomain string) p2p.HostStatus {
	if a.p2pAPI == nil {
		return p2p.HostStatus{}
	}
	return a.p2pAPI.GetP2PHostStatus(subdomain)
}

// ListP2PHosts повертає стан УСІХ активних хостингових сесій (мультихостинг:
// кілька збірок одночасно).
func (a *App) ListP2PHosts() []p2p.HostStatus {
	if a.p2pAPI == nil {
		return nil
	}
	return a.p2pAPI.ListP2PHosts()
}

// HostP2PConnections повертає, ХТО зараз під'єднаний до конкретного хоста і
// з яким пінгом.
func (a *App) HostP2PConnections(subdomain string) []p2p.HostConnection {
	if a.p2pAPI == nil {
		return nil
	}
	return a.p2pAPI.HostConnections(subdomain)
}

// ── UDP-тунель голосового чату (галочка в UI) ───────────────────────────

// SetP2PUDP вмикає/вимикає UDP-тунель голосового чату для ПРИЄДНАННЯ
// (гість). Хостингова сторона передає свій порт аргументом StartP2PHost.
func (a *App) SetP2PUDP(enabled bool, voicePort int) error {
	if a.p2pAPI == nil {
		return nil
	}
	if a.p2pDisabled() && enabled {
		return errP2PDisabled()
	}
	// Гість (приєднання) не залежить від акаунта Шаурми.
	return a.p2pAPI.SetP2PUDP(enabled, voicePort)
}

// GetP2PUDPPref — збережені UDP-налаштування сервера (serverID або "port-N").
// found=false — ще не зберігались (UI може мігрувати старе значення з localStorage).
func (a *App) GetP2PUDPPref(serverID string) map[string]any {
	if a.p2pDisabled() || a.p2pAPI == nil {
		return map[string]any{"found": false, "enabled": false, "port": 0}
	}
	p, ok := a.p2pAPI.GetUDPPref(serverID)
	return map[string]any{"found": ok, "enabled": p.Enabled, "port": p.Port}
}

// SetP2PUDPPref — ПОСТІЙНО зберігає UDP-налаштування сервера (port=0 → «як порт сервера»).
func (a *App) SetP2PUDPPref(serverID string, enabled bool, port int) error {
	if a.p2pDisabled() {
		return errP2PDisabled()
	}
	return a.p2pAPI.SetUDPPref(serverID, enabled, port)
}

// GetP2PUDPStatus — стан UDP-тунелю для UI.
func (a *App) GetP2PUDPStatus() p2p.P2PUDPStatus {
	if a.p2pAPI == nil {
		return p2p.P2PUDPStatus{}
	}
	return a.p2pAPI.GetP2PUDPStatus()
}

// ReleaseP2PAddress явно звільняє адресу (видаляє її з lobby і з локальної
// книги адрес).
func (a *App) ReleaseP2PAddress(subdomain string) error {
	if a.p2pDisabled() {
		return errP2PDisabled()
	}
	return a.p2pAPI.ReleaseP2PAddress(subdomain)
}

// ── Guest (фоновий слухач для приєднання до чужих LAN-світів) ───────────

// EnsureP2PGuestService піднімає фоновий гостьовий слухач (127.0.0.2:25565),
// якщо він ще не запущений. Викликається один раз при старті лаунчера.
// Не піднімаємо НІЧОГО, якщо P2P вимкнено в налаштуваннях.
func (a *App) EnsureP2PGuestService() error {
	if a.p2pAPI == nil {
		return nil
	}
	if a.p2pDisabled() {
		return nil
	}
	// Приєднання до чужих світів працює БЕЗ акаунта Шаурми (join потребує лише токена лаунчера).
	return a.p2pAPI.EnsureP2PGuestService()
}

// ApplyP2PToggle застосовує вимикач P2P ОДРАЗУ після збереження
// налаштувань — без перезапуску лаунчера.
//
// Гість не залежить від акаунта Шаурми: слухач вмикається/гаситься лише за вимикачем.
func (a *App) ApplyP2PToggle() {
	if a.p2pAPI == nil {
		return
	}
	enabled := !a.p2pDisabled()
	if err := a.p2pAPI.SetGuestEnabled(enabled); err != nil {
		log.Printf("p2p: застосування вимикача: %v", err)
	}
}

// ── Оренда хоста, співхости, спільні кімнати, списки доступу ────────────

// wireP2PEvents підключає події вузла до UI: тунель зупинено не користувачем (оренду забрано /
// співхоста відкликано) → "p2p:host_stopped" {subdomain, reason}. Викликається один раз після NewAPI.
func (a *App) wireP2PEvents() {
	if a.p2pAPI == nil {
		return
	}
	a.p2pAPI.OnHostStopped = func(subdomain, reason string) {
		a.emit("p2p:host_stopped", map[string]string{"subdomain": subdomain, "reason": reason})
	}
}

// P2PHostStartCohost — співхост запускає хостинг чужої спільної кімнати.
func (a *App) P2PHostStartCohost(minecraftPort, address string, udpPort int) (p2p.HostStatus, error) {
	if a.p2pDisabled() {
		return p2p.HostStatus{Error: errP2PDisabled().Error()}, errP2PDisabled()
	}
	_, ownerName, err := a.p2pAccountCheck()
	if err != nil {
		return p2p.HostStatus{Error: err.Error()}, err
	}
	return a.p2pAPI.StartCohostSession(minecraftPort, address, udpPort, ownerName)
}

// P2PHostStop — зупинка хостингу (віддає оренду). Те саме, що StopP2PHost.
func (a *App) P2PHostStop(address string) error { return a.p2pAPI.StopP2PHost(address) }

// P2PSetAccess міняє режим доступу кімнати. Працює і без запущеного сервера:
// режим зберігається в самій кімнаті й застосується, коли хостинг
// підніметься (раніше це було можливо лише під час активної сесії — те саме
// «баг: доступ не можна редагувати, коли сервер не запущений»).
func (a *App) P2PSetAccess(subdomain, access string) error {
	if a.p2pAPI == nil {
		return nil
	}
	return a.p2pAPI.SetAccessMode(subdomain, access)
}

// P2PRoomAccess — поточний режим доступу кімнати (для перемикача в UI, який
// має бути коректним і при зупиненому сервері). Помилку ковтаємо: невідомий
// режим краще показати як порожній, ніж валити вкладку «Мережа».
func (a *App) P2PRoomAccess(subdomain string) string {
	if a.p2pAPI == nil {
		return ""
	}
	access, err := a.p2pAPI.RoomAccess(subdomain)
	if err != nil {
		return ""
	}
	return access
}
func (a *App) P2PKick(address string) (bool, error) { return a.p2pAPI.KickHost(address) }
func (a *App) P2PShared() ([]p2p.SharedRoom, error) { return a.p2pAPI.SharedRooms() }
func (a *App) P2PCohosts(address string) (p2p.CohostList, error) {
	return a.p2pAPI.Cohosts(address)
}
func (a *App) P2PCohostsApply(address string, add, remove []p2p.ACLEntry) (p2p.CohostApplyResult, error) {
	return a.p2pAPI.CohostApply(address, add, remove)
}
func (a *App) P2PACL(address string) (p2p.ACLList, error) { return a.p2pAPI.ACLList(address) }
func (a *App) P2PACLApply(address string, add, remove []p2p.ACLEntry) (p2p.ACLApplyResult, error) {
	return a.p2pAPI.ACLApply(address, add, remove)
}

// SetP2PRejectTexts приймає від фронтенду переклади повідомлень, які лаунчер показує гравцеві в
// самій грі, коли його не пустили на P2P-сервер (хост офлайн, «лише друзі», немає доступу…).
// Ключі — p2p.RejectKeys(), значення — текст мовою лаунчера (разом з імпортованими мовами).
// Порожній набір повертає вбудовані uk/en. Нічого не пише на диск і не ходить у мережу.
func (a *App) SetP2PRejectTexts(texts map[string]string) {
	p2p.SetRejectTexts(texts)
}

// GetP2PRejectKeys — ключі повідомлень про відмову (для фронтенду й перевірки перекладів).
func (a *App) GetP2PRejectKeys() []string {
	return p2p.RejectKeys()
}
