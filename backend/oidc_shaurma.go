//go:build shaurma

package main

// Вхід у Google/Microsoft ДЛЯ АКАУНТА ШАУРМИ — окремий від входу в Minecraft.
//
// Беремо ЛИШЕ id_token зі scope «openid»: це підписаний «паспорт» («ти — користувач X»),
// який не дає права на пошту, диск, Xbox чи Minecraft. Access- і refresh-токени провайдера,
// що приходять разом у відповіді, одразу відкидаємо — вони ніде не зберігаються й нікуди
// не передаються. id_token іде на VPS лише на реєстрації; той перевіряє підпис, бере sub,
// зберігає тільки його хеш і забуває токен. nonce прив'язує id_token до ключа комірки, тож
// вкрадений токен не можна використати з іншим ключем.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/auth"
)

// Налаштування клієнтів OAuth (публічні ідентифікатори додатка, не секрети користувача).
// Задаються при збірці: -ldflags "-X main.shaurmaGoogleClientID=... -X main.shaurmaGoogleClientSecret=..."
// Для Google потрібен клієнт типу «Desktop app»; його «секрет» за дизайном Google не конфіденційний.
// redirect_uri ОДИН для Google і Microsoft: http://localhost:29111/auth (auth.CallbackRedirectURI).
// Якщо клієнт Google — типу «Web application», цей URI треба додати в «Authorized redirect URIs».
// Ті самі client_id мають бути в GOOGLE_CLIENT_IDS / MICROSOFT_CLIENT_IDS на VPS.
var (
	shaurmaGoogleClientID     = ""
	shaurmaGoogleClientSecret = ""
	shaurmaMSClientID         = "90bdddf4-ae0e-456b-ac6f-64b366a0b596"
)

var (
	oidcMu     sync.Mutex
	oidcCancel context.CancelFunc
)

type oidcProvider struct {
	authURL, tokenURL, clientID, secret string
}

func oidcProviderFor(name string) (oidcProvider, error) {
	switch name {
	case "google":
		if shaurmaGoogleClientID == "" {
			return oidcProvider{}, errors.New("вхід через Google не налаштовано в цій збірці (немає googleClientId у build-secrets.json)")
		}
		return oidcProvider{"https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token",
			shaurmaGoogleClientID, shaurmaGoogleClientSecret}, nil
	case "microsoft":
		return oidcProvider{"https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize",
			"https://login.microsoftonline.com/consumers/oauth2/v2.0/token", shaurmaMSClientID, ""}, nil
	}
	return oidcProvider{}, errors.New("невідомий провайдер входу")
}

func decodeJSONLimited(r io.Reader, v any) error {
	return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v)
}

func randURL(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// oidcIDToken — shaurmaid.IDTokenSource: відкриває браузер, чекає код, міняє його на id_token.
// Сесію сторінки в браузері (підсумок входу) віддає oidcIDTokenSession.
func (a *App) oidcIDToken(ctx context.Context, providerName, nonce string) (string, error) {
	tok, _, err := a.oidcIDTokenSession(ctx, providerName, nonce)
	return tok, err
}

// oidcIDTokenSession — як oidcIDToken, але повертає й сесію спільного callback-сервера: через неї
// викликач повідомляє сторінці в браузері підсумок («вхід» / «реєстрація» / помилка).
// Сесія повертається живою лише при успіху; при помилці вона вже завершена/закрита.
func (a *App) oidcIDTokenSession(ctx context.Context, providerName, nonce string) (string, *auth.Session, error) {
	tok, sess, err := a.oidcIDTokenRaw(ctx, providerName, nonce)
	if err != nil {
		if ctx.Err() == nil {
			return "", nil, &oidcLocalError{msg: err.Error()}
		}
		return "", nil, err
	}
	return tok, sess, nil
}

// oidcLocalError — помилка самого входу (браузер/Google/Microsoft), НЕ мережі до сервера
// Шаурми. Показуємо як є, а не «немає зв'язку з сервером Шаурми».
type oidcLocalError struct{ msg string }

func (e *oidcLocalError) Error() string { return e.msg }

func (a *App) oidcIDTokenRaw(ctx context.Context, providerName, nonce string) (string, *auth.Session, error) {
	p, err := oidcProviderFor(providerName)
	if err != nil {
		return "", nil, err
	}
	// ОДИН loopback-порт і ОДИН шлях для Google і Microsoft (див. auth/loopback.go):
	// http://localhost:29111/auth. Саме цей redirect_uri треба один раз додати в
	// «Authorized redirect URIs» клієнта Google — інакше «Error 400: redirect_uri_mismatch».
	redirect := auth.CallbackRedirectURI()
	state := randURL(24)
	verifier := randURL(48)
	ch := sha256.Sum256([]byte(verifier))

	sess, err := auth.Callback.Begin(state, providerName)
	if err != nil {
		return "", nil, err
	}
	// Будь-який вихід із помилкою → сторінка в браузері отримує підсумок «не вдалося», а при
	// скасуванні з лаунчера (ctx) сервер просто закривається й порт звільняється.
	ok := false
	defer func() {
		if ok {
			return
		}
		if ctx.Err() != nil {
			sess.Close()
		}
	}()
	fail := func(msg string) (string, *auth.Session, error) {
		sess.Finish(auth.Outcome{Phase: "error", Provider: providerName, Message: msg})
		return "", nil, errors.New(msg)
	}

	u := p.authURL + "?" + url.Values{
		"client_id":             {p.clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirect},
		"scope":                 {"openid"}, // ЛИШЕ openid
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(ch[:])},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}.Encode()
	a.OpenExternal(u)

	var code string
	select {
	case r := <-sess.Result():
		if r.Err != nil {
			// Сторінку про скасування/помилку callback-обробник уже показав.
			sess.Close()
			return "", nil, errors.New("вхід скасовано")
		}
		code = r.Code
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}

	form := url.Values{
		"client_id": {p.clientID}, "grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier},
	}
	if p.secret != "" {
		form.Set("client_secret", p.secret)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		return fail("немає зв'язку з сервісом входу")
	}
	defer resp.Body.Close()
	// Читаємо тіло цілком (до 1 МБ), щоб при помилці показати справжню причину від провайдера,
	// а не безлике «не вдалось отримати підтвердження входу».
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr struct {
		IDToken string `json:"id_token"`
		// access_token / refresh_token у відповіді є, але ми їх НЕ читаємо й НЕ зберігаємо.
		Error     string `json:"error"`
		ErrorDesc string `json:"error_description"`
	}
	jerr := json.Unmarshal(body, &tr)
	if resp.StatusCode != http.StatusOK || jerr != nil || tr.IDToken == "" {
		// У журнал помилок лаунчера — деталі (БЕЗ тіла відповіді: воно може містити токени).
		detail := "provider=" + providerName + " http=" + strconv.Itoa(resp.StatusCode) +
			" error=" + tr.Error + " description=" + tr.ErrorDesc
		if resp.StatusCode == http.StatusOK && tr.IDToken == "" && jerr == nil {
			detail += " (у відповіді немає id_token)"
		}
		a.logErrorDetail("oidcTokenExchange", "обмін коду на id_token не вдався", detail)
		return fail(oidcExchangeMessage(providerName, resp.StatusCode, tr.Error, tr.ErrorDesc, p.secret == ""))
	}
	ok = true
	return tr.IDToken, sess, nil
}

// oidcExchangeMessage — зрозуміле повідомлення за відповіддю token-endpoint провайдера.
func oidcExchangeMessage(provider string, status int, code, desc string, noSecret bool) string {
	base := "не вдалось отримати підтвердження входу"
	d := strings.ToLower(desc)
	switch {
	case provider == "google" && (strings.Contains(d, "client_secret") || (code == "invalid_request" && noSecret)):
		return base + ": Google вимагає client secret, а в цій збірці його не вшито (googleClientSecret)"
	case code == "redirect_uri_mismatch" || strings.Contains(d, "redirect_uri"):
		return base + ": redirect_uri не збігається — додай " + auth.CallbackRedirectURI() + " в Authorized redirect URIs клієнта"
	case code == "invalid_client":
		return base + ": невірний client id/secret клієнта OAuth (invalid_client)"
	case code == "invalid_grant":
		return base + ": код входу недійсний або прострочений — спробуй увійти ще раз"
	case code != "":
		if desc != "" {
			return base + ": " + code + " — " + desc
		}
		return base + ": " + code
	case status != http.StatusOK:
		return base + " (HTTP " + strconv.Itoa(status) + ")"
	}
	return base + ": провайдер не повернув id_token"
}
