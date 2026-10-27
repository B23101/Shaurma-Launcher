//go:build shaurma

package main

// authlib_injector.go — скіни для ОФЛАЙН (неліцензійних) акаунтів.
//
// Механізм (єдиний, який реально працює у ванільному клієнті):
// лаунчер запускає JVM з java-агентом authlib-injector, який підміняє в
// клієнті URL сесійного сервісу на наш worker (Yggdrasil-API —
// worker/yggdrasil.js). Клієнт питає профіль у «сесійного сервера» й
// отримує ПІДПИСАНУ властивість textures з URL нашого PNG → показує скін.
//
// ЧОГО НЕ РОБИМО (історія): раніше код копіював PNG у
// {gameDir}/assets/skins/{нік}.png і вважав, що клієнт це прочитає. Ванільний
// клієнт такий шлях НЕ читає взагалі (скіни беруться лише з профілю сесійного
// сервера), тому в грі все одно був Steve — фіча виглядала робочою, але не
// працювала. Файлову ін'єкцію прибрано (див. docs/dev/SKINS_SYSTEM_INSTRUCTION.md §4).
//
// Що передаємо JVM (специфікація authlib-injector, «Adding Launch Arguments»):
//   -javaagent:<jar>=<API root>            — підміна сесійного сервісу
//   -Dauthlibinjector.yggdrasil.prefetched=<base64 metadata>
//                                          — metadata заздалегідь, щоб гра
//                                            не ходила в мережу на старті
//
// Jar НЕ вбудовуємо в бінарник (authlib-injector під AGPL-3.0): качаємо з
// нашого CDN, якщо Pack Manager його туди поклав, інакше — з офіційного
// релізу на GitHub. Файл кешується у <dataDir>/java/ і переюзується.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/update"
)

const (
	// Версія офіційного релізу — фолбек, коли jar'а немає на нашому CDN.
	// Оновлювати разом із перевіркою сумісності з поточними MC-версіями.
	authlibInjectorVersion = "1.2.8"
	authlibInjectorJarName = "authlib-injector.jar"

	// Префікс нашого Yggdrasil-API (VPS-шлюз проксіює його у worker/yggdrasil.js).
	// У JVM аргумент іде З КІНЦЕВИМ СЛЕШЕМ (див. authlibJVMArgs).
	yggdrasilAPIPath = "/api/yggdrasil"

	// Мінімальний правдоподібний розмір jar (~600 КБ у релізі). Захист від
	// того, щоб HTML-сторінку помилки зберегли як «jar» і передали в JVM.
	minAuthlibJarSize = 50 * 1024
	// Стеля, щоб підмінений CDN не залив у теку лаунчера гігабайти.
	maxAuthlibJarSize = 8 * 1024 * 1024
)

// javaToolsDir — тека допоміжних java-інструментів лаунчера.
func (a *App) javaToolsDir() string { return filepath.Join(a.cfg.Dir(), "java") }

// authlibInjectorJar повертає шлях до authlib-injector.jar, завантажуючи
// його за потреби. Викликається при КОЖНОМУ запуску гри (для будь-якого
// типу акаунта — офлайн, Microsoft, Шаурма), поки скіни не вимкнено в
// налаштуваннях (DisableSkins).
// authlibFetchFn — точка підміни мережевих завантажень у тестах (у продакшн-збірці
// завжди ходить у мережу через fetchWithToken/fetchPublic). Дозволяє перевірити, що
// збій підготовки інжектора — лише попередження, без опори на реальну мережу.
var authlibFetchFn = func(a *App, url string, withToken bool) ([]byte, error) {
	if withToken {
		return a.fetchWithToken(url)
	}
	return a.fetchPublic(url)
}

func (a *App) authlibInjectorJar() (string, error) {
	dst := filepath.Join(a.javaToolsDir(), authlibInjectorJarName)
	if st, err := os.Stat(dst); err == nil && st.Size() >= minAuthlibJarSize {
		return dst, nil
	}
	if err := os.MkdirAll(a.javaToolsDir(), 0o755); err != nil {
		return "", fmt.Errorf("не вдалося створити %s: %w", a.javaToolsDir(), err)
	}

	// 1) Наш CDN — якщо Pack Manager задеплоїв компонент
	//    launcher/authlib-injector.jar (тоді версію контролюємо ми).
	body, err := authlibFetchFn(a, update.CDNBaseURL()+"/launcher/"+authlibInjectorJarName, true)
	if err != nil || !looksLikeJar(body) {
		// 2) Офіційний реліз — без редистрибуції jar'а з нашого боку.
		ver := strings.TrimPrefix(authlibInjectorVersion, "v")
		url := fmt.Sprintf(
			"https://github.com/yushijinhun/authlib-injector/releases/download/v%s/authlib-injector-%s.jar",
			ver, ver)
		body, err = authlibFetchFn(a, url, false)
		if err != nil {
			return "", fmt.Errorf("завантаження authlib-injector (%s): %w", url, err)
		}
	}
	if !looksLikeJar(body) {
		return "", fmt.Errorf("authlib-injector: отриманий файл не є jar (%d байт)", len(body))
	}

	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

// looksLikeJar перевіряє, що тіло — ZIP-архів (jar — це zip) правдоподібного
// розміру. Повної перевірки підпису немає: файл іде по HTTPS з нашого CDN
// або з офіційного релізу.
func looksLikeJar(body []byte) bool {
	if len(body) < minAuthlibJarSize || len(body) > maxAuthlibJarSize {
		return false
	}
	return bytes.HasPrefix(body, []byte{0x50, 0x4b, 0x03, 0x04}) // PK\x03\x04
}

// yggdrasilPrefetched повертає base64 від metadata нашого Yggdrasil-API.
// Мережа — ПЕРЕД кешем: ключ підпису на worker'і може змінитися (ротація,
// виправлення зіпсованого секрета), а кеш на диску колись записувався
// назавжди. Зі старим public key клієнт відкидав би підпис textures — і в
// грі лишався Steve без ЖОДНОГО рядка в лозі (тихо, найгірший вигляд бага).
// Кеш використовується лише як фолбек, коли мережі немає на старті гри:
// тоді гра запускається з останньої відомої metadata замість відмови.
// На некоректну/безключову відповідь кеш НЕ застосовуємо: він міг би
// «сховати», що ключ на сервері змінився чи взагалі зник.
func (a *App) yggdrasilPrefetched(base string, allowCache bool) (string, error) {
	cache := filepath.Join(a.javaToolsDir(), "yggdrasil-metadata.b64")
	cached := func() string {
		if data, err := os.ReadFile(cache); err == nil {
			if s := strings.TrimSpace(string(data)); len(s) > 16 {
				return s
			}
		}
		return ""
	}

	body, err := authlibFetchFn(a, strings.TrimRight(base, "/")+yggdrasilAPIPath, false)
	if err != nil {
		// Кеш — лише для ОСТАННЬОГО вузла: інакше впалий вузол «вдавав би» успіх
		// і наступний не спробували б.
		if allowCache {
			if s := cached(); s != "" {
				return s, nil
			}
		}
		return "", fmt.Errorf("Yggdrasil-API недоступний (%s): %w", base, err)
	}
	var meta struct {
		SignaturePublickey string   `json:"signaturePublickey"`
		SkinDomains        []string `json:"skinDomains"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("Yggdrasil-API: некоректна metadata: %w", err)
	}
	if strings.TrimSpace(meta.SignaturePublickey) == "" {
		// Ключ підпису не заданий у секретах worker'а — інжектор не має
		// сенсу: підписані textures клієнт однаково відкине.
		return "", fmt.Errorf("Yggdrasil-API без ключа підпису (YGGDRASIL_KEY_JWK не задано у worker'і)")
	}

	b64 := base64.StdEncoding.EncodeToString(body)
	_ = os.MkdirAll(a.javaToolsDir(), 0o755)
	_ = os.WriteFile(cache, []byte(b64), 0o644) // кеш необов'язковий — помилку ігноруємо
	return b64, nil
}

// authlibJVMArgs — аргументи JVM скін-системи. ТІЛЬКИ для offline-акаунтів.
//
// ВАЖЛИВО (фікс бага): для акаунтів account.Type == "microsoft" injector
// НЕ підключається і функція одразу повертає (nil, nil). Причина: authlib-
// injector підміняє ввесь Yggdrasil session API (login/hasJoined/join/
// profile) на apiRoot нашого worker'а — не лише текстури. Якщо підключити
// його для ліцензійного (Microsoft) акаунта, JVM перестає звертатись до
// справжнього sessionserver.mojang.com, і:
//  1. офіційні сервери, що виконують справжній Mojang hasJoined-callback,
//     більше не бачать дійсну сесію гравця → вхід на ліцензійні сервери
//     блокується;
//  2. профіль/текстури такого гравця так само йдуть через наш worker,
//     тому замість свого скіна гравець бачить підмінені дані.
//
// Виклик цієї функції для Microsoft-акаунта — і є першопричиною обох
// симптомів одночасно. Порожній результат для offline-акаунта = інжектор
// недоступний (мережа/налаштування) — гра має запуститись як звичайно,
// тому помилку повертаємо разом із причиною.
func (a *App) authlibJVMArgs(account model.Account) ([]string, error) {
	return a.authlibJVMArgsOpt(account, false)
}

// authlibJVMArgsOpt — те саме, але allowLicensed=true дозволяє підключити
// injector і ЛІЦЕНЗІЙНОМУ акаунту. Викликається ЛИШЕ коли гравець (або адмін
// пака) свідомо обрав для ліцензії систему скінів «Шаурма» — з попередженням,
// що вхід на сервери з перевіркою Mojang (Hypixel тощо) тоді недоступний.
func (a *App) authlibJVMArgsOpt(account model.Account, allowLicensed bool) ([]string, error) {
	if !allowLicensed && (account.IsLicensed || account.Type == "microsoft") {
		// Ліцензійний акаунт: жодного втручання в auth. JVM запускається
		// без -javaagent і йде напряму на справжній Mojang/Microsoft API.
		return nil, nil
	}
	jar, err := a.authlibInjectorJar()
	if err != nil {
		return nil, err
	}
	base, prefetched, err := a.yggdrasilEndpoint()
	if err != nil {
		return nil, err
	}
	// Кінцевий слеш ОБОВ'ЯЗКОВИЙ: authlib-injector дописує до API root
	// відносні шляхи ("sessionserver/session/minecraft/profile/…") — без
	// слеша останній сегмент з'ївся б як ім'я файлу, і запити пішли б на
	// /api/sessionserver/… (404).
	apiRoot := strings.TrimRight(base, "/") + yggdrasilAPIPath + "/"

	// Локальний injector з двома потоками (skin_proxy.go): гра → 127.0.0.1 →
	// Shaurma, а якщо скіна там немає — Mojang за ніком. Будь-яка помилка
	// старту проксі НЕ фатальна: тихо лишається старий прямий шлях нижче.
	if !a.cfg.GetSettings().DisableMojangSkinFallback {
		var nicks []string
		nicks = append(nicks, account.Username)
		for _, acc := range a.cfg.GetAccounts() {
			nicks = append(nicks, acc.Username)
		}
		if root, meta, perr := a.ensureSkinProxy(base, prefetched, nicks); perr == nil {
			return []string{
				"-javaagent:" + jar + "=" + root,
				"-Dauthlibinjector.yggdrasil.prefetched=" + meta,
			}, nil
		} else {
			launchlog.Warn("SkinProxy", "локальний injector недоступний, пряма схема Shaurma: %v", perr)
		}
	}
	return []string{
		"-javaagent:" + jar + "=" + apiRoot,
		"-Dauthlibinjector.yggdrasil.prefetched=" + prefetched,
	}, nil
}

// yggdrasilEndpoint обирає, КУДИ гра ходитиме за скінами, і повертає базу разом
// із metadata саме з неї (skinDomains у metadata залежить від хоста, з якого її
// взято, тому база й metadata мусять збігатися).
//
// Використовуються лише VPS-вузли кластера. Якщо недоступні всі, для останнього
// з них береться кеш metadata на диску.
func (a *App) yggdrasilEndpoint() (base, prefetched string, err error) {
	vpsPool().Prime()
	bases := vpsPool().GatewayBases("ygg")
	if len(bases) == 0 {
		return "", "", errors.New("Yggdrasil-API: жоден вузол кластера не відомий")
	}
	var firstErr error
	for i, b := range bases {
		if p, e := a.yggdrasilPrefetched(b, i == len(bases)-1); e == nil {
			return b, p, nil
		} else if firstErr == nil {
			firstErr = e
		}
	}
	return "", "", firstErr
}

// ── HTTP-хелпери ──────────────────────────────────────────────────────
// Заголовки ті самі, що в решті запитів лаунчера: worker віддає файли
// лише за User-Agent «ShaurmaLauncher/…» + X-Shaurma-Token.
func (a *App) fetchWithToken(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	req.Header.Set("X-Shaurma-Token", update.Token())
	return doFetch(req)
}

// fetchPublic — для публічних ендпоінтів (/api/yggdrasil, GitHub-реліз):
// токен не потрібен, а GitHub на чужий токен відповів би 404.
func (a *App) fetchPublic(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	return doFetch(req)
}

func doFetch(req *http.Request) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("HTTP %d від %s", resp.StatusCode, req.URL.Host)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAuthlibJarSize))
}
