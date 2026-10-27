package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc — мінімальний RoundTripper для тестів: замість реального
// HTTPS-запиту до generativelanguage.googleapis.com повертає заготовлену
// відповідь (як HTTP-клієнт з кастомним транспортом).
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestGemini401FriendlyMessage — 401 (недійсний ключ) від Gemini має
// перетворюватись на зрозуміле повідомлення з підказкою, а не на сирий
// JSON від Google («Request had invalid authentication credentials...»),
// який користувач не може розшифрувати.
func TestGemini401FriendlyMessage(t *testing.T) {
	c := &Client{
		apiKey:  "invalid-key",
		models:  DefaultModels,
		timeout: 5 * 1000 * 1000 * 1000, // 5s
		http: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Body: io.NopCloser(strings.NewReader(`{
						"error": {
							"code": 401,
							"message": "Request had invalid authentication credentials."
						}
					}`)),
					Header: make(http.Header),
				}, nil
			}),
		},
	}
	_, err := c.AnalyzeCrash(context.Background(), "uk", CrashContext{LogWindow: "test"})
	if err == nil {
		t.Fatal("очікували помилку при 401, отримали успіх")
	}
	msg := err.Error()
	if !strings.Contains(msg, "недійсний Gemini API-ключ") {
		t.Fatalf("повідомлення має пояснювати недійсний ключ, отримали: %s", msg)
	}
	if strings.Contains(msg, "invalid authentication") || strings.Contains(msg, "error") && !strings.Contains(msg, "HTTP 401") {
		t.Fatalf("повідомлення не має містити сирий JSON Google: %s", msg)
	}
	if !strings.Contains(msg, "Налаштуваннях") {
		t.Fatalf("повідомлення має підказувати, де вказати ключ: %s", msg)
	}
}

// TestGemini500KeepsTechnical — 500 (внутрішня помилка сервісу) НЕ має
// трактуватись як проблема ключа: лишається технічне повідомлення.
func TestGemini500KeepsTechnical(t *testing.T) {
	c := &Client{
		apiKey:  "key",
		models:  DefaultModels,
		timeout: 5 * 1000 * 1000 * 1000,
		http: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Status:     "500 Internal Server Error",
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":500}}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}
	_, err := c.AnalyzeCrash(context.Background(), "uk", CrashContext{LogWindow: "test"})
	if err == nil {
		t.Fatal("очікували помилку при 500")
	}
	if strings.Contains(err.Error(), "недійсний Gemini API-ключ") {
		t.Fatalf("500 не має трактуватись як проблема ключа: %s", err.Error())
	}
}

// TestGroq401FriendlyMessage — той самий френдлі-401 для Groq-клієнта
// (аналіз крашу йде через той самий chatOnce, що переклад).
func TestGroq401FriendlyMessage(t *testing.T) {
	c := &GroqClient{
		apiKey:  "invalid-key",
		models:  []string{DefaultGroqModel},
		timeout: 5 * 1000 * 1000 * 1000,
		http: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid api key"}}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}
	_, err := c.AnalyzeCrash(context.Background(), "uk", CrashContext{LogWindow: "test"})
	if err == nil {
		t.Fatal("очікували помилку при 401")
	}
	if !strings.Contains(err.Error(), "недійсний Groq API-ключ") {
		t.Fatalf("повідомлення має пояснювати недійсний ключ: %s", err.Error())
	}
}

// TestGroqDecommissionedModelFallsBackToNext — захист від регресу: коли
// перша модель у списку знята з обслуговування Groq (model_decommissioned,
// саме так одного разу llama-3.3-70b-versatile перестав відповідати
// повністю — див. коментар біля DefaultGroqModels), клієнт має ОДРАЗУ
// перейти до наступної моделі зі списку, а не витрачати всі 3 спроби
// ретраю на модель, яка ніколи не відповість успіхом, і НЕ повертати
// користувачу помилку, якщо наступна модель робоча.
func TestGroqDecommissionedModelFallsBackToNext(t *testing.T) {
	var calledModels []string
	c := &GroqClient{
		apiKey:  "valid-key",
		models:  []string{"llama-3.3-70b-versatile", "openai/gpt-oss-120b"},
		timeout: 5 * 1000 * 1000 * 1000,
		http: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				var parsed struct {
					Model string `json:"model"`
				}
				_ = json.Unmarshal(body, &parsed)
				calledModels = append(calledModels, parsed.Model)

				if parsed.Model == "llama-3.3-70b-versatile" {
					return &http.Response{
						StatusCode: http.StatusBadRequest,
						Status:     "400 Bad Request",
						Body: io.NopCloser(strings.NewReader(
							`{"error":{"code":"model_decommissioned","message":"model decommissioned"}}`)),
						Header: make(http.Header),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body: io.NopCloser(strings.NewReader(
						`{"choices":[{"message":{"content":"ok"}}]}`)),
					Header: make(http.Header),
				}, nil
			}),
		},
	}
	out, err := c.chat(context.Background(), "system", "user", 100, false)
	if err != nil {
		t.Fatalf("очікували успіх через fallback на другу модель, отримали помилку: %v", err)
	}
	if out != "ok" {
		t.Fatalf("out = %q, очікувано \"ok\"", out)
	}
	if len(calledModels) != 2 || calledModels[0] != "llama-3.3-70b-versatile" || calledModels[1] != "openai/gpt-oss-120b" {
		t.Fatalf("очікували виклик [llama-3.3-70b-versatile, openai/gpt-oss-120b], отримали: %v (перша знята з обслуговування модель має спробуватись РІВНО РАЗ, без марних ретраїв)", calledModels)
	}
}
