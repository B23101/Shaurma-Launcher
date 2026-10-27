package social

import (
	"errors"
	"testing"
	"time"
)

func newTestLimiter() (*sendLimiter, *time.Time) {
	now := time.Unix(1_700_000_000, 0)
	l := &sendLimiter{}
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterBurstPerChat(t *testing.T) {
	l, now := newTestLimiter()
	for i := 0; i < chatBurstMax; i++ {
		if err := l.allowMessage("a_b", string(rune('a'+i))); err != nil {
			t.Fatalf("msg %d should pass: %v", i, err)
		}
	}
	if err := l.allowMessage("a_b", "zzz"); !errors.Is(err, ErrChatSpam) {
		t.Fatalf("burst must be blocked, got %v", err)
	}
	// інший діалог не страждає
	if err := l.allowMessage("a_c", "hi"); err != nil {
		t.Fatalf("other chat must pass: %v", err)
	}
	// після вікна знову можна
	*now = now.Add(chatBurstWindow + time.Second)
	if err := l.allowMessage("a_b", "again"); err != nil {
		t.Fatalf("after window must pass: %v", err)
	}
}

func TestLimiterDuplicates(t *testing.T) {
	l, now := newTestLimiter()
	for i := 0; i < dupMax; i++ {
		if err := l.allowMessage("a_b", "Привіт"); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(chatBurstWindow + time.Second) // щоб не впертись у burst
	}
	if err := l.allowMessage("a_b", "  привіт "); !errors.Is(err, ErrChatSpam) {
		t.Fatalf("duplicate must be blocked, got %v", err)
	}
	*now = now.Add(dupWindow + time.Second)
	if err := l.allowMessage("a_b", "привіт"); err != nil {
		t.Fatalf("after dup window must pass: %v", err)
	}
}

func TestLimiterRejectedNotCounted(t *testing.T) {
	l, now := newTestLimiter()
	for i := 0; i < chatBurstMax; i++ {
		_ = l.allowMessage("a_b", string(rune('a'+i)))
	}
	for i := 0; i < 50; i++ {
		_ = l.allowMessage("a_b", "x") // усі відхилені
	}
	*now = now.Add(chatBurstWindow + time.Second)
	if err := l.allowMessage("a_b", "ok"); err != nil {
		t.Fatalf("rejected attempts must not extend the block: %v", err)
	}
}

func TestLimiterGlobal(t *testing.T) {
	l, now := newTestLimiter()
	for i := 0; i < globalMax; i++ {
		chat := string(rune('A' + i%26))
		if i >= 26 {
			chat += "2"
		}
		if err := l.allowMessage(chat, "m"+string(rune('a'+i%26))+string(rune('a'+i/26))); err != nil {
			// допускаємо burst/dup лише якщо це не наш тест
			t.Fatalf("msg %d: %v", i, err)
		}
		*now = now.Add(10 * time.Millisecond)
	}
	if err := l.allowMessage("zz", "new"); !errors.Is(err, ErrChatSpam) {
		t.Fatalf("global limit must block, got %v", err)
	}
}

func TestTypingThrottle(t *testing.T) {
	l, now := newTestLimiter()
	if !l.allowTyping("a_b") {
		t.Fatal("first typing must pass")
	}
	if l.allowTyping("a_b") {
		t.Fatal("immediate second typing must be dropped")
	}
	*now = now.Add(typingMinGap + time.Millisecond)
	if !l.allowTyping("a_b") {
		t.Fatal("typing after gap must pass")
	}
}

func TestErrorCodeChatSpam(t *testing.T) {
	if got := errorCode(ErrChatSpam); got != "chat_spam" {
		t.Fatalf("got %q", got)
	}
}
