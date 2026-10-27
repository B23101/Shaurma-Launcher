package social

// sendlimit.go — антиспам чату на боці лаунчера.
//
// Кожне надіслане повідомлення — це запит до VPS і Worker (ліміт Cloudflare
// на весь акаунт спільний для всіх користувачів), тож «залипла» клавіша Enter,
// скрипт чи просто нетерплячий користувач не повинні їсти спільну квоту.
// Ліміти навмисно щедрі для нормальної розмови й нечутливі для людини, але
// зупиняють потік:
//
//   - у ОДНОМУ діалозі: не більше chatBurstMax повідомлень за chatBurstWindow;
//   - у ВСІХ діалогах разом: не більше globalMax за globalWindow;
//   - ОДНАКОВИЙ текст у діалозі: не більше dupMax разів за dupWindow.
//
// Відхилене повідомлення НЕ рахується (щоб відмова не продовжувала блокування)
// і до мережі не доходить: перевірка йде ДО шифрування й черги.

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrChatSpam — надсилаємо надто часто. UI показує переклад за кодом chat_spam.
var ErrChatSpam = errors.New("social:chat_spam")

const (
	chatBurstMax    = 5
	chatBurstWindow = 8 * time.Second

	globalMax    = 30
	globalWindow = time.Minute

	dupMax    = 3
	dupWindow = 30 * time.Second

	// typing: не частіше одного разу на typingMinGap у діалозі (UI й так шле раз на 3 с).
	typingMinGap = 2 * time.Second
)

type sentMark struct {
	at   time.Time
	text string // нормалізований (trim + lower); порожній для не-текстових
}

type sendLimiter struct {
	mu      sync.Mutex
	perChat map[string][]sentMark
	global  []time.Time
	typing  map[string]time.Time
	now     func() time.Time // підмінюється в тестах
}

func (l *sendLimiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func keepAfter(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	return ts[i:]
}

// allowMessage перевіряє й, якщо можна, ЗАРАХОВУЄ повідомлення.
func (l *sendLimiter) allowMessage(chatID, text string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	if l.perChat == nil {
		l.perChat = make(map[string][]sentMark)
	}

	// глобальне вікно
	l.global = keepAfter(l.global, now.Add(-globalWindow))
	if len(l.global) >= globalMax {
		return ErrChatSpam
	}

	// вікно діалогу (зберігаємо все, що в межах найдовшого вікна — dupWindow)
	keepFrom := now.Add(-dupWindow)
	marks := l.perChat[chatID]
	j := 0
	for j < len(marks) && !marks[j].at.After(keepFrom) {
		j++
	}
	marks = marks[j:]

	burstFrom := now.Add(-chatBurstWindow)
	norm := strings.ToLower(strings.TrimSpace(text))
	burst, dups := 0, 0
	for _, m := range marks {
		if m.at.After(burstFrom) {
			burst++
		}
		if norm != "" && m.text == norm {
			dups++
		}
	}
	if burst >= chatBurstMax || dups >= dupMax {
		l.perChat[chatID] = marks
		return ErrChatSpam
	}

	l.perChat[chatID] = append(marks, sentMark{at: now, text: norm})
	l.global = append(l.global, now)
	return nil
}

// allowTyping — true, якщо індикатор друку можна відправити (інакше мовчки пропускаємо).
func (l *sendLimiter) allowTyping(chatID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	if l.typing == nil {
		l.typing = make(map[string]time.Time)
	}
	if last, ok := l.typing[chatID]; ok && now.Sub(last) < typingMinGap {
		return false
	}
	// не даємо мапі рости: чистимо застарілі
	if len(l.typing) > 256 {
		for k, v := range l.typing {
			if now.Sub(v) > time.Minute {
				delete(l.typing, k)
			}
		}
	}
	l.typing[chatID] = now
	return true
}
