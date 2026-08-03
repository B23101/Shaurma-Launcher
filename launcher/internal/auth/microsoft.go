package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/model"
)

// Microsoft OAuth — як у старому лаунчері (і HMCL):
//  1. Authorization Code + PKCE через login.microsoftonline.com/consumers,
//     локальний сервер на портах 29111-29115. Код захоплюється АВТОМАТИЧНО
//     з callback-запиту — копіювати його вручну не треба.
//  2. Якщо жоден порт не вільний — помилка (device code тут не потрібен,
//     порти майже завжди вільні на робочій станції).
//
// login.live.com/consumers НЕ перевіряє redirect_uri для localhost —
// реєстрація в Azure не потрібна, client ID беремо зі старого лаунчера.
const (
	msClientID = "90bdddf4-ae0e-456b-ac6f-64b366a0b596"
	msAuthURL  = "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize"
	msTokenURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	scope      = "XboxLive.signin offline_access"
	callback   = "/auth"
)

var callbackPorts = []int{29111, 29112, 29113, 29114, 29115}

const (
	xblAuthURL   = "https://user.auth.xboxlive.com/user/authenticate"
	xstsAuthURL  = "https://xsts.auth.xboxlive.com/xsts/authorize"
	mcLoginURL   = "https://api.minecraftservices.com/authentication/login_with_xbox"
	mcProfileURL = "https://api.minecraftservices.com/minecraft/profile"
)

type loginResult struct {
	code string
	err  error
}

type Authenticator struct {
	client *http.Client

	mu       sync.Mutex
	listener net.Listener
	srv      *http.Server
	verifier string
	state    string
	redirect string
	resultCh chan loginResult
}

func NewAuthenticator() *Authenticator {
	return &Authenticator{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// StartLogin запускає локальний callback-сервер і повертає URL для
// відкриття в браузері користувача.
func (a *Authenticator) StartLogin() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ln, port, err := bindLocalServer()
	if err != nil {
		return "", err
	}
	a.listener = ln
	a.resultCh = make(chan loginResult, 1)
	a.verifier = generateCodeVerifier()
	challenge := generateCodeChallenge(a.verifier)
	a.state = randomBase64(16)
	a.redirect = fmt.Sprintf("http://localhost:%d%s", port, callback)

	mux := http.NewServeMux()
	mux.HandleFunc(callback, a.handleCallback)
	a.srv = &http.Server{Handler: mux}
	go func() { _ = a.srv.Serve(ln) }()

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
	return msAuthURL + "?" + params.Encode(), nil
}

// AwaitLogin блокується, поки браузер не перенаправить на локальний
// callback (або не спливе таймаут 5 хв), потім завершує авторизацію.
func (a *Authenticator) AwaitLogin() (*model.Account, error) {
	var res loginResult
	select {
	case res = <-a.resultCh:
	case <-time.After(5 * time.Minute):
		a.close()
		return nil, fmt.Errorf("час очікування авторизації вичерпано")
	}
	a.close()
	if res.err != nil {
		return nil, res.err
	}
	return a.completeAuthWithCode(res.code)
}

// CancelLogin перериває очікування AwaitLogin (скасування в UI).
func (a *Authenticator) CancelLogin() {
	a.mu.Lock()
	ch := a.resultCh
	a.mu.Unlock()
	if ch != nil {
		select {
		case ch <- loginResult{err: fmt.Errorf("авторизацію скасовано")}:
		default:
		}
	}
	a.close()
}

func (a *Authenticator) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	errorCode := q.Get("error")
	errorDesc := q.Get("error_description")

	success := false
	var result loginResult
	switch {
	case errorCode != "":
		result.err = fmt.Errorf("OAuth помилка: %s %s", errorCode, errorDesc)
	case state == "" || state != a.state:
		result.err = fmt.Errorf("невідповідність state (CSRF)")
	case code == "":
		result.err = fmt.Errorf("немає коду авторизації в callback")
	default:
		success = true
		result.code = code
	}

	a.mu.Lock()
	if a.resultCh != nil {
		a.resultCh <- result
	}
	a.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, resultHTML(success))
	go a.close()
}

func (a *Authenticator) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = a.srv.Shutdown(ctx)
		cancel()
		a.srv = nil
	}
	if a.listener != nil {
		_ = a.listener.Close()
		a.listener = nil
	}
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
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("обмін коду не вдався (%d): %s", resp.StatusCode, string(respBody))
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("немає access_token: %s", tokens.Error)
	}
	return a.completeAuth(tokens.AccessToken, tokens.RefreshToken)
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
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("refresh не вдався (%d): %s", resp.StatusCode, string(respBody))
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("немає access_token після refresh")
	}
	return a.completeAuth(tokens.AccessToken, tokens.RefreshToken)
}

// completeAuth: XBL → XSTS → launcher/login → profile.
func (a *Authenticator) completeAuth(msAccess, msRefresh string) (*model.Account, error) {
	xbl, err := a.authenticateXBL(msAccess)
	if err != nil {
		return nil, err
	}
	xsts, uhs, err := a.authenticateXSTS(xbl)
	if err != nil {
		return nil, err
	}
	mcToken, expiresIn, err := a.loginMinecraft(uhs, xsts)
	if err != nil {
		return nil, err
	}
	profile, err := a.getProfile(mcToken)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second).UnixMilli()
	return &model.Account{
		ID:            profile.ID,
		Username:      profile.Name,
		Type:          "microsoft",
		AccessToken:   mcToken,
		MsAccessToken: msAccess,
		RefreshToken:  msRefresh,
		ExpiresAt:     expiresAt,
		UUID:          profile.ID,
		IsLicensed:    true,
	}, nil
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
		return "", err
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
		return "", "", err
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
			return "", "", fmt.Errorf("акаунт не прив'язаний до Xbox Live")
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
		return "", 86400, err
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
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("немає ліцензії Minecraft Java Edition")
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

func bindLocalServer() (net.Listener, int, error) {
	for _, port := range callbackPorts {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln, port, nil
		}
	}
	return nil, 0, fmt.Errorf("жоден порт %v не вільний", callbackPorts)
}

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

func resultHTML(success bool) string {
	accent, msg, hint := "#00C851", "Авторизацію завершено!", "Можна повернутися в лаунчер."
	badgeIcon := `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12l5 5l10 -10"/></svg>`
	if !success {
		accent, msg, hint = "#FF4757", "Помилка авторизації.", "Повернись у лаунчер і спробуй ще раз."
		badgeIcon = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6l-12 12M6 6l12 12"/></svg>`
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="uk">
<head>
<meta charset="utf-8">
<title>Shaurma Launcher</title>
<style>
  *{margin:0;padding:0;box-sizing:border-box}
  body{
    background:
      radial-gradient(900px 500px at 12%% -5%%,rgba(255,138,0,.07),transparent 60%%),
      radial-gradient(900px 600px at 100%% 110%%,rgba(255,94,0,.08),transparent 55%%),
      #0f0f0f;
    color:#f5f5f5;
    font-family:'Inter','Segoe UI',system-ui,sans-serif;
    height:100vh;display:flex;align-items:center;justify-content:center;
  }
  .card{
    width:min(92vw,400px);
    background:#161616;border:1px solid rgba(255,255,255,.09);
    border-radius:16px;padding:38px 34px;text-align:center;
    box-shadow:0 24px 60px rgba(0,0,0,.5);
  }
  .brand{display:flex;align-items:center;justify-content:center;gap:10px}
  .brand .logo{
    width:34px;height:34px;border-radius:10px;
    display:flex;align-items:center;justify-content:center;
    background:linear-gradient(135deg,#ff8a00,#ff5e00);
    box-shadow:0 8px 18px rgba(255,138,0,.3);
  }
  .brand .logo svg{width:19px;height:19px;color:#fff}
  .brand .txt{text-align:left}
  .brand .name{font-size:15px;font-weight:800;letter-spacing:.3px}
  .brand .sub{font-size:8.5px;font-weight:700;color:#a0a0a0;letter-spacing:1.8px}
  .badge{
    width:78px;height:78px;border-radius:50%%;margin:28px auto 20px;
    display:flex;align-items:center;justify-content:center;
    background:rgba(255,255,255,.03);border:1px solid rgba(255,255,255,.08);
    color:%s;
  }
  .badge svg{width:36px;height:36px}
  .msg{font-size:16.5px;font-weight:800}
  .hint{font-size:12.5px;color:#a0a0a0;margin-top:8px;line-height:1.5}
  .foot{margin-top:28px;padding-top:14px;border-top:1px solid rgba(255,255,255,.06);font-size:10.5px;color:#5c6570;letter-spacing:.4px;text-transform:uppercase}
</style>
</head>
<body>
  <div class="card">
    <div class="brand">
      <div class="logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8.5 14.5A2.5 2.5 0 0 0 11 12c0-1.38-.5-2-1-3-1.072-2.143-.224-4.054 2-6 .5 2.5 2 4.9 4 6.5 2 1.6 3 3.5 3 5.5a7 7 0 1 1-14 0c0-1.153.433-2.294 1-3a2.5 2.5 0 0 0 2.5 2.5z"/></svg></div>
      <div class="txt">
        <div class="name">Shaurma</div>
        <div class="sub">LAUNCHER</div>
      </div>
    </div>
    <div class="badge">%s</div>
    <div class="msg">%s</div>
    <div class="hint">%s</div>
    <div class="foot">Microsoft&nbsp;OAuth</div>
  </div>
</body>
</html>`, accent, badgeIcon, msg, hint)
}
