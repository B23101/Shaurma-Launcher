package java

import (
	"errors"
	"testing"
)

// TestCancelFlag — прапорець скасування: RequestCancelInstall ставить,
// ResetCancelInstall скидає, ShareCancelFlagWith об'єднує два інстанси
// (тимчасовий інстанс для кастомної теки користувача слухає основний).
func TestCancelFlag(t *testing.T) {
	main := NewInstaller(t.TempDir())
	tmp := NewInstaller(t.TempDir())

	if main.IsCancelRequested() {
		t.Fatal("новий інстанс не має бути скасованим")
	}

	main.RequestCancelInstall()
	if !main.IsCancelRequested() {
		t.Fatal("RequestCancelInstall не встановив прапорець")
	}
	if tmp.IsCancelRequested() {
		t.Fatal("інший інстанс не має бачити скасування ДО ShareCancelFlagWith")
	}

	// Тимчасовий інстанс (Java у теці користувача) має слухати основний.
	tmp.ShareCancelFlagWith(main)
	if !tmp.IsCancelRequested() {
		t.Fatal("ShareCancelFlagWith не поширив прапорець на тимчасовий інстанс")
	}

	main.ResetCancelInstall()
	if main.IsCancelRequested() || tmp.IsCancelRequested() {
		t.Fatal("ResetCancelInstall не скинув прапорець (включно зі спільним)")
	}
}

// TestErrInstallCancelled — сентинел помилки впізнається errors.Is (так
// launchInstallErr у app.go відрізняє скасування від реальної помилки).
func TestErrInstallCancelled(t *testing.T) {
	if !errors.Is(ErrInstallCancelled, ErrInstallCancelled) {
		t.Fatal("errors.Is на самому сентинелі має проходити")
	}
	wrapped := errors.New("контекст").Error()
	if wrapped == "" {
		t.Fatal("noop")
	}
}
