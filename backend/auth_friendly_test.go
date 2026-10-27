package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"shaurma-launcher-backend/internal/auth"
)

func TestWrapRefreshErr(t *testing.T) {
	rej := fmt.Errorf("refresh не вдався (400): AADSTS70000: %w", auth.ErrRefreshRejected)
	got := wrapRefreshErr("token refresh", rej)
	if !errors.Is(got, auth.ErrRefreshRejected) {
		t.Fatal("має зберігати ErrRefreshRejected")
	}
	if !strings.Contains(got.Error(), "Налаштування → Акаунти") || !strings.Contains(got.Error(), "AADSTS70000") {
		t.Fatalf("немає інструкції або технічної причини: %v", got)
	}
	other := wrapRefreshErr("token refresh", errors.New("boom"))
	if other.Error() != "token refresh: boom" {
		t.Fatalf("інша помилка не має змінюватись: %v", other)
	}
}
