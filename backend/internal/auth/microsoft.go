package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/neterr"
)

// Microsoft OAuth — як у старому лаунчері (і HMCL):
//  1. Authorization Code + PKCE через login.microsoftonline.com/consumers,
//     спільний локальний сервер (loopback.go) на ОДНОМУ порту — той самий,
//     що й для входу через Google. Код захоплюється АВТОМАТИЧНО з
//     callback-запиту — копіювати його вручну не треба.
//  2. Якщо порт зайнятий іншою програмою — помилка (device code тут не
//     потрібен, порт майже завжди вільний на робочій станції).
//
// login.live.com/consumers НЕ перевіряє redirect_uri для localhost —
// реєстрація в Azure не потрібна, client ID беремо зі старого лаунчера.
const (
	msClientID = "90bdddf4-ae0e-456b-ac6f-64b366a0b596"
	msAuthURL  = "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize"
	msTokenURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	// openid/profile/email — щоб Microsoft повернув id_token. Це ЄДИНЕ, що
	// лаунчер віддає на сервер Шаурми для входу (особа = sub із id_token);
	// XboxLive.signin лишається для запуску гри (XBL→XSTS→Minecraft), а
	// offline_access — щоб мати refresh-токен Microsoft.
	scope = "openid profile email XboxLive.signin offline_access"
)

// ErrRefreshRejected — Microsoft остаточно відхилив refresh-токен (400/401/403:
// invalid_grant, відкликаний, сплив). Потрібен повторний вхід в акаунт, на
// відміну від тимчасових мережевих збоїв.
var ErrRefreshRejected = errors.New("microsoft: refresh-токен відхилено")

const (
	xblAuthURL   = "https://user.auth.xboxlive.com/user/authenticate"
	xstsAuthURL  = "https://xsts.auth.xboxlive.com/xsts/authorize"
	mcLoginURL   = "https://api.minecraftservices.com/authentication/login_with_xbox"
	mcProfileURL = "https://api.minecraftservices.com/minecraft/profile"
)

type Authenticator struct {
	client *http.Client

	mu sync.Mutex
	// sess — поточна сесія спільного loopback-сервера (див. loopback.go).
	sess     *Session
	verifier string
	state    string
	redirect string
}

func NewAuthenticator() *Authenticator {
	return &Authenticator{
		client: &http.Client{
			Timeout: 30 * time.Second,
			// Власний транспорт замість http.DefaultTransport: на Linux
			// у мережах без справжнього IPv6 (типово — домашній
			// провайдер без нативного v6) з'єднання з
			// login.microsoftonline.com може спершу піти в AAAA-адресу і
			// висіти до системного TCP-таймауту. FallbackDelay вмикає
			// Happy Eyeballs (RFC 6555): якщо IPv6 не відповів за 150мс,
			// паралельно йде IPv4 — обмін коду на токени перестає
			// «залипати» на десятки секунд.
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:       10 * time.Second,
					KeepAlive:     30 * time.Second,
					FallbackDelay: 150 * time.Millisecond,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          8,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: time.Second,
			},
		},
	}
}

// StartLogin запускає спільний локальний callback-сервер і повертає URL для
// відкриття в браузері користувача.
//
// ВАЖЛИВО: спершу закриваємо ПОПЕРЕДНЮ сесію, якщо вона ще висить (користувач
// закрив вікно входу до завершення). Порт один, тож без цього наступний вхід
// упирався б у порт, зайнятий САМИМ лаунчером. Callback.Begin робить це сам.
func (a *Authenticator) StartLogin() (string, error) {
	return a.StartLoginWithNonce("")
}

// StartLoginWithNonce — як StartLogin, але кладе nonce прямо в запит авторизації. Microsoft
// повертає його всередині id_token, тож цей самий вхід одразу підтверджує й акаунт Шаурми
// (nonce прив'язаний до ключа комірки): другий вхід у браузері не потрібен.
// Порожній nonce — поведінка як раніше (id_token без прив'язки до ключа).
func (a *Authenticator) StartLoginWithNonce(nonce string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := randomBase64(16)
	sess, err := Callback.Begin(state, "microsoft")
	if err != nil {
		return "", err
	}
	a.sess = sess
	a.verifier = generateCodeVerifier()
	challenge := generateCodeChallenge(a.verifier)
	a.state = state
	a.redirect = CallbackRedirectURI()

	params := url.Values{
		"client_id":             {msClientID},
		"response_type":         {"code"},
		"redirect_uri":          {a.redirect},
		"scope":                 {scope},
		"prompt":                {"select_account"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {a.state},
	}
	if nonce != "" {
		params.Set("nonce", nonce)
	}
	return msAuthURL + "?" + params.Encode(), nil
}

// Session — поточна сесія входу (для Finish підсумку на сторінці в браузері).
// Викликати ДО AwaitLogin: після нього сесія може бути вже замінена наступним входом.
func (a *Authenticator) Session() *Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sess
}

// AwaitLogin блокується, поки браузер не перенаправить на локальний
// callback (або не спливе таймаут 5 хв), потім завершує авторизацію.
//
// Сервер після callback НЕ закривається: сторінка в браузері чекає підсумок, який
// викликач повідомляє через Session.Finish. Якщо завершення входу тут упало —
// сторінка отримує помилку одразу. Виняток — NoLicenseError: це не збій, і підсумок
// для нього визначає викликач.
func (a *Authenticator) AwaitLogin() (*model.Account, error) {
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess == nil {
		return nil, fmt.Errorf("вхід не розпочато")
	}

	var res CallbackResult
	select {
	case res = <-sess.Result():
	case <-time.After(5 * time.Minute):
		sess.Close()
		return nil, fmt.Errorf("час очікування авторизації вичерпано")
	}
	if res.Err != nil {
		// Сторінку про помилку/скасування callback-обробник уже показав; Close — на випадок,
		// якщо це Cancel (тоді сервер уже закритий, виклик безпечний).
		sess.Close()
		return nil, res.Err
	}
	acc, err := a.completeAuthWithCode(res.Code)
	if err != nil {
		var nl *NoLicenseError
		if !errors.As(err, &nl) {
			sess.Finish(Outcome{Phase: "error", Provider: "microsoft",
				Message: "Не вдалося завершити вхід у Microsoft."})
		}
		return nil, err
	}
	return acc, nil
}

// CancelLogin перериває очікування AwaitLogin (скасування в UI) і звільняє порт.
func (a *Authenticator) CancelLogin() {
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	sess.Cancel()
}

func (a *Authenticator) completeAuthWithCode(code string) (*model.Account, error) {
	body := "client_id=" + url.QueryEscape(msClientID) +
		"&code=" + url.QueryEscape(code) +
		"&grant_type=authorization_code" +
		"&code_verifier=" + url.QueryEscape(a.verifier) +
		"&redirect_uri=" + url.QueryEscape(a.redirect) +
		"&scope=" + url.QueryEscape(scope)

	resp, err := a.client.Post(msTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		return nil, neterr.Friendly(err, "увійти в акаунт Microsoft")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("обмін коду не вдався (%d): %s", resp.StatusCode, string(respBody))
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("немає access_token: %s", tokens.Error)
	}
	return a.completeAuth(tokens.AccessToken, tokens.RefreshToken, tokens.IDToken)
}

// RefreshTokens оновлює прострочені токени Microsoft (гра або звичайний
// вхід після перезапуску лаунчера) — повертає оновлений акаунт.
func (a *Authenticator) RefreshTokens(refreshToken string) (*model.Account, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("немає refresh token")
	}
	body := "client_id=" + url.QueryEscape(msClientID) +
		"&refresh_token=" + url.QueryEscape(refreshToken) +
		"&grant_type=refresh_token" +
		"&scope=" + url.QueryEscape(scope)

	resp, err := a.client.Post(msTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		return nil, neterr.Friendly(err, "оновити сесію Microsoft")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("refresh не вдався (%d): %s: %w", resp.StatusCode, string(respBody), ErrRefreshRejected)
		}
		return nil, fmt.Errorf("refresh не вдався (%d): %s", resp.StatusCode, string(respBody))
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("немає access_token після refresh")
	}
	// id_token із refresh не має nonce комірки Шаурми й для реєстрації не годиться — не несемо далі.
	return a.completeAuth(tokens.AccessToken, tokens.RefreshToken, "")
}

// Причини, чому вхід у Microsoft вдався, а грати з цього акаунта не можна.
var (
	errNoXboxProfile = fmt.Errorf("акаунт не прив'язаний до Xbox Live")
	errNoJavaLicense = fmt.Errorf("немає ліцензії Minecraft Java Edition")
)

// NoLicenseError — Microsoft-вхід УСПІШНИЙ (особу підтверджено), але в акаунта немає Minecraft
// Java Edition (або ще немає профілю Xbox). Це не збій входу: лаунчер пропонує створити
// неліцензійний акаунт Шаурми, прив'язаний до цього Microsoft. IDToken — «паспорт» цього входу
// (nonce комірки вже вшито), Reason — "no_license" | "no_xbox".
type NoLicenseError struct {
	IDToken string
	Reason  string
}

func (e *NoLicenseError) Error() string {
	if e.Reason == "no_xbox" {
		return errNoXboxProfile.Error()
	}
	return errNoJavaLicense.Error()
}

// completeAuth: XBL → XSTS (гра) + XSTS (соціальні Xbox) → launcher/login
// → profile.
func (a *Authenticator) completeAuth(msAccess, msRefresh, idToken string) (*model.Account, error) {
	xbl, err := a.authenticateXBL(msAccess)
	if err != nil {
		return nil, err
	}
	xsts, uhs, err := a.authenticateXSTS(xbl)
	if err != nil {
		if errors.Is(err, errNoXboxProfile) && idToken != "" {
			return nil, &NoLicenseError{IDToken: idToken, Reason: "no_xbox"}
		}
		return nil, err
	}
	mcToken, expiresIn, err := a.loginMinecraft(uhs, xsts)
	if err != nil {
		return nil, err
	}
	profile, err := a.getProfile(mcToken)
	if err != nil {
		if errors.Is(err, errNoJavaLicense) && idToken != "" {
			return nil, &NoLicenseError{IDToken: idToken, Reason: "no_license"}
		}
		return nil, err
	}

	acc := &model.Account{
		ID:            profile.ID,
		Username:      profile.Name,
		Type:          "microsoft",
		AccessToken:   mcToken,
		MsAccessToken: msAccess,
		RefreshToken:  msRefresh,
		ExpiresAt:     time.Now().Add(time.Duration(expiresIn) * time.Second).UnixMilli(),
		UUID:          profile.ID,
		IsLicensed:    true,
		// id_token — особа Microsoft для входу в Шаурму (сервер перевіряє
		// підпис і бере sub). Порожній, якщо Microsoft не повернув (напр.
		// старий кеш scope) — тоді вхід у Шаурму просто недоступний.
		MsIDToken: idToken,
	}

	// Соціальний XSTS (релей http://xboxlive.com) — для розділу «Друзі»
	// (People Hub / presence). Best-effort: якщо Xbox соціал недоступний
	// (країна, дитячий акаунт, тимчасовий збій) — вхід у гру НЕ блокуємо,
	// просто лишаємо поля порожніми (друзі покажуть «недоступно»).
	socialToken, socialUHS, xuid, gt, serr := a.authenticateSocialXSTS(xbl)
	if serr == nil {
		acc.XblToken = socialToken
		acc.XblUHS = socialUHS
		acc.XboxUserID = xuid
		acc.Gamertag = gt
	}
	return acc, nil
}

// RefreshSocial отримує СВІЖІ соціальні Xbox-токени (XBL → XSTS для
// http://xboxlive.com) за Microsoft access token. Використовується, коли
// збережений XblToken відсутній або прострочений (напр. після перезапуску
// лаунчера, коли соціальний XSTS уже недійсний).
func (a *Authenticator) RefreshSocial(msAccess string) (token, uhs, xuid, gt string, err error) {
	xbl, err := a.authenticateXBL(msAccess)
	if err != nil {
		return "", "", "", "", err
	}
	return a.authenticateSocialXSTS(xbl)
}

// authenticateSocialXSTS отримує XSTS-токен для релею http://xboxlive.com
// (Xbox Social / People Hub) — на додачу до стандартного
// rp://api.minecraftservices.com/, який використовується лише для самої
// гри. Claims DisplayClaims.xui містять uhs, xid (xuid) і gt (gamertag).
func (a *Authenticator) authenticateSocialXSTS(xblToken string) (token, uhs, xuid, gt string, err error) {
	payload := map[string]interface{}{
		"Properties": map[string]interface{}{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
		"RelyingParty": "http://xboxlive.com",
		"TokenType":    "JWT",
	}
	data, _ := json.Marshal(payload)
	resp, err := a.client.Post(xstsAuthURL, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return "", "", "", "", neterr.Friendly(err, "увійти через Xbox Live")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			XErr int64 `json:"XErr"`
		}
		_ = json.Unmarshal(body, &e)
		switch e.XErr {
		case 2148916233:
			return "", "", "", "", fmt.Errorf("акаунт не прив'язаний до Xbox Live")
		case 2148916235:
			return "", "", "", "", fmt.Errorf("Xbox Live недоступний у вашій країні")
		case 2148916238:
			return "", "", "", "", fmt.Errorf("дитячий акаунт — потрібен дозвіл батьків")
		}
		return "", "", "", "", fmt.Errorf("social XSTS error (%d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		Token         string `json:"Token"`
		DisplayClaims struct {
			XUI []struct {
				UHS string `json:"uhs"`
				XID string `json:"xid"`
				GT  string `json:"gt"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", "", "", err
	}
	if len(result.DisplayClaims.XUI) == 0 {
		return "", "", "", "", fmt.Errorf("no user hash in social XSTS")
	}
	x := result.DisplayClaims.XUI[0]
	return result.Token, x.UHS, x.XID, x.GT, nil
}

func (a *Authenticator) authenticateXBL(accessToken string) (string, error) {
	payload := map[string]interface{}{
		"Properties": map[string]interface{}{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + accessToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}
	data, _ := json.Marshal(payload)
	resp, err := a.client.Post(xblAuthURL, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return "", neterr.Friendly(err, "увійти через Xbox Live")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("XBL error (%d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		Token string `json:"Token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return result.Token, nil
}

func (a *Authenticator) authenticateXSTS(xblToken string) (string, string, error) {
	payload := map[string]interface{}{
		"Properties": map[string]interface{}{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}
	data, _ := json.Marshal(payload)
	resp, err := a.client.Post(xstsAuthURL, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return "", "", neterr.Friendly(err, "отримати доступ до Minecraft")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			XErr int64 `json:"XErr"`
		}
		_ = json.Unmarshal(body, &e)
		switch e.XErr {
		case 2148916233:
			return "", "", errNoXboxProfile
		case 2148916235:
			return "", "", fmt.Errorf("Xbox Live недоступний у вашій країні")
		case 2148916238:
			return "", "", fmt.Errorf("дитячий акаунт — потрібен дозвіл батьків")
		}
		return "", "", fmt.Errorf("XSTS error (%d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		Token         string `json:"Token"`
		DisplayClaims struct {
			XUI []struct {
				UHS string `json:"uhs"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", err
	}
	if len(result.DisplayClaims.XUI) == 0 {
		return "", "", fmt.Errorf("no user hash")
	}
	return result.Token, result.DisplayClaims.XUI[0].UHS, nil
}

func (a *Authenticator) loginMinecraft(userHash, xstsToken string) (string, int64, error) {
	payload := map[string]string{
		"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", userHash, xstsToken),
	}
	data, _ := json.Marshal(payload)
	resp, err := a.client.Post(mcLoginURL, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return "", 86400, neterr.Friendly(err, "увійти в Minecraft")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", 86400, fmt.Errorf("MC login error (%d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 86400, err
	}
	if result.ExpiresIn == 0 {
		result.ExpiresIn = 86400
	}
	return result.AccessToken, result.ExpiresIn, nil
}

func (a *Authenticator) getProfile(accessToken string) (*struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}, error) {
	req, _ := http.NewRequest("GET", mcProfileURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, neterr.Friendly(err, "завантажити профіль Minecraft")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return nil, errNoJavaLicense
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("profile error (%d): %s", resp.StatusCode, string(body))
	}
	var profile struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

func generateCodeVerifier() string {
	b := make([]byte, 64)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func randomBase64(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
