package custompack

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func sha1Hex(b []byte) string {
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

// TestDownloadOneFileRangeResume проверяет ключевую часть «пауза не теряет
// прогресс»: если .part уже существует, downloadOneFile докачивает файл с
// места остановки (Range-заголовок) и объединяет sha1 существующей части с
// новой — финальный файл идентичен полной загрузке.
func TestDownloadOneFileRangeResume(t *testing.T) {
	content := []byte("shaurma-launcher resume test payload: 0123456789")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Header.Get("Range") != "" {
			// Сервер поддерживает Range: отдаём с запрошенного байта.
			rangeVal := r.Header.Get("Range")
			parts := strings.Split(strings.TrimPrefix(rangeVal, "bytes="), "-")
			start, _ := strconv.ParseInt(parts[0], 10, 64)
			w.Header().Set("Content-Range", "bytes "+parts[0]+"-"+strconv.FormatInt(int64(len(content))-1, 10)+"/"+strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(content[start:])
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "mods", "example.jar")
	os.MkdirAll(filepath.Dir(dest), 0755)

	// 1) Первый запуск: пишем .part вручную на середину (эмуляция того,
	//    что краш/пауза произошли на половине загрузки).
	half := len(content) / 2
	if err := os.WriteFile(dest+".part", content[:half], 0644); err != nil {
		t.Fatal(err)
	}

	// 2) «Продолжить»: downloadOneFile должен докачать остаток.
	task := importTask{
		relPath: "mods/example.jar",
		url:     server.URL,
	}
	client := &http.Client{}
	var bytesRead int64
	_, err := downloadOneFile(context.Background(), client, task, dir, func(delta int64) {
		bytesRead += delta
	})
	if err != nil {
		t.Fatalf("downloadOneFile resume failed: %v", err)
	}
	// Докачан был только хвост, а не весь файл.
	if bytesRead != int64(len(content)-half) {
		t.Fatalf("resume должен скачать %d байт, получили %d", len(content)-half, bytesRead)
	}
	// Финальный файл целостный.
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("final content mismatch: %q != %q", got, content)
	}
	// .part переименован в финальный файл.
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Fatal(".part должен исчезнуть после успешного завершения")
	}
}

// TestDownloadOneFileResumeSha1Verification проверяет, что при resume с
// заданным sha1 итоговый хеш считается по ВСЕМУ файлу (существующая часть +
// докачанная), а не только по хвосту — иначе валидный файл ложно
// отбраковывался бы.
func TestDownloadOneFileResumeSha1Verification(t *testing.T) {
	content := []byte("resume with sha1 check across partial + tail")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			// Range: bytes=N- — старт після першого '-', N може бути з
			// суфіксом "-" (відкритий діапазон).
			raw := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
			dash := strings.IndexByte(raw, '-')
			start, _ := strconv.ParseInt(raw[:dash], 10, 64)
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(int64(len(content))-1, 10)+"/"+strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(content[start:])
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	// sha1 от всего содержимого.
	sha1Hex := sha1Hex(content)

	dir := t.TempDir()
	dest := filepath.Join(dir, "mods", "x.jar")
	os.MkdirAll(filepath.Dir(dest), 0755)
	half := len(content) / 2
	if err := os.WriteFile(dest+".part", content[:half], 0644); err != nil {
		t.Fatal(err)
	}

	task := importTask{relPath: "mods/x.jar", url: server.URL, sha1: sha1Hex}
	_, err := downloadOneFile(context.Background(), &http.Client{}, task, dir, nil)
	if err != nil {
		t.Fatalf("resume с sha1 должен пройти: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(content) {
		t.Fatalf("content mismatch: %q", got)
	}
}
