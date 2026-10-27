package minecraft

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDownloadFileCancelIsPrompt проверяет, что кнопка «Зупинити» мягко
// прерывает загрузку компонентов игры: downloadFile возвращает
// ErrInstallCancelled, а частичный файл остаётся на диске как .part (для
// резюме через Range), а не удаляется.
func TestDownloadFileCancelIsPrompt(t *testing.T) {
	// Сервер отдаёт файл медленно (блоками с задержкой), чтобы тест успел
	// нажать «Стоп» посреди загрузки.
	payload := make([]byte, 8*1024*1024) // 8 МБ
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "8388608")
		w.WriteHeader(200)
		for i := 0; i < 128; i++ {
			if _, err := w.Write(payload[i*65536 : (i+1)*65536]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(8 * time.Millisecond)
		}
	}))
	defer ts.Close()

	dir := t.TempDir()
	inst := NewInstaller(dir)
	inst.ResetCancelInstall()

	// Нажимаем «Стоп» почти сразу — через 30мс загрузка в разгаре.
	go func() {
		time.Sleep(30 * time.Millisecond)
		inst.RequestCancelInstall()
	}()

	dest := filepath.Join(dir, "libraries", "test.jar")
	err := inst.downloadFile(ts.URL, dest)
	if !errors.Is(err, ErrInstallCancelled) {
		t.Fatalf("downloadFile должен вернуть ErrInstallCancelled, получили: %v", err)
	}

	// Частичный .part остаётся на диске (не удаляется) — повторный запуск
	// докачает его через Range, а не начнёт с нуля.
	if _, statErr := os.Stat(dest + ".part"); statErr != nil {
		t.Fatalf("после отмены .part должен остаться на диске, stat err: %v", statErr)
	}
	// Финального файла быть не должно.
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("финальный файл не должен существовать после отмены")
	}
}

// TestDownloadFileSha1Mismatch проверяет, что передача ожидаемого sha1 в
// downloadFile ловит повреждённый файл (режим «Полагодити» перекачивает
// битые библиотеки).
func TestDownloadFileSha1Mismatch(t *testing.T) {
	// Реальные данные: sha1("hello") = aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello")
	}))
	defer ts.Close()

	dir := t.TempDir()
	inst := NewInstaller(dir)
	dest := filepath.Join(dir, "lib.jar")
	err := inst.downloadFile(ts.URL, dest, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("ожидали ошибку sha1 mismatch, получили nil")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("битый файл должен быть удалён после sha1 mismatch")
	}
}
