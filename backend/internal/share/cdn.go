package share

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Завантаження файлів, які відправник знайшов на Modrinth, напряму з CDN — паралельно з
// P2P. Канал друга віддає лише унікальне (конфіги, світи, власні моди), а мод із
// Modrinth приходить з їхнього CDN. Довіра — лише до sha256 з маніфесту відправника:
// CDN не може нічого підмінити, а будь-який збій (мережа, 404, інший вміст) означає,
// що той самий файл просто піде через P2P.

// errCDNFallback — файл не вдалося взяти з CDN, його треба качати через P2P.
var errCDNFallback = errors.New("cdn: відкат на P2P")

// cdnStallTimeout — без жодного байта стільки часу → вважаємо CDN недоступним для файлу.
const cdnStallTimeout = 30 * time.Second

func newCDNClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		// Переадресацію дозволяємо лише в межах https-хостів Modrinth.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || req.URL.Scheme != "https" || !isModrinthHost(req.URL.Hostname()) {
				return errors.New("cdn: недозволена переадресація")
			}
			return nil
		},
	}
}

func isModrinthHost(h string) bool {
	h = strings.ToLower(h)
	return h == "modrinth.com" || strings.HasSuffix(h, ".modrinth.com")
}

// fetchCDN — до двох спроб узяти файл i з CDN. nil — прийнято й звірено; errCDNFallback —
// треба через P2P; інша помилка — фатальна (диск, скасування).
func (d *Downloader) fetchCDN(parent context.Context, m *Manifest, i int) error {
	fe := m.Files[i]
	if fe.Src == nil || !ValidCDNURL(fe.Src.URL) {
		return errCDNFallback
	}
	for attempt := 0; attempt < 2; {
		if err := d.waitIfPaused(parent); err != nil {
			return err
		}
		if d.isDone(i) {
			return nil
		}
		err := d.cdnOnce(parent, fe, i)
		switch {
		case err == nil:
			return nil
		case parent.Err() != nil:
			return parent.Err()
		case errors.Is(err, errPausedLocal):
			continue // пауза — не спроба
		}
		var fatal fatalErr
		if errors.As(err, &fatal) {
			return err
		}
		var hf hashFail
		if errors.As(err, &hf) {
			return errCDNFallback // інший вміст — повторювати з CDN марно
		}
		attempt++
		if serr := sleepCtx(parent, time.Duration(attempt)*700*time.Millisecond); serr != nil {
			return serr
		}
	}
	return errCDNFallback
}

func (d *Downloader) cdnOnce(parent context.Context, fe FileEntry, i int) error {
	final, err := d.safePath(fe.Path)
	if err != nil {
		return fatalErr{err}
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return fatalErr{diskErr(err)}
	}
	part := final + ".part"
	ctx, cancel := d.reqCtx(parent)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fe.Src.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher (share)")
	d.setCurrent(fe.Path)
	resp, err := d.cdn.Do(req)
	if err != nil {
		return d.pauseAware(parent, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cdn: відповідь %d", resp.StatusCode)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != fe.Size {
		return hashFail{fe.Path}
	}

	// Сторожовий таймер: CDN, що «завис» без жодного байта, не тримає файл вічно.
	stall := time.AfterFunc(cdnStallTimeout, cancel)
	defer stall.Stop()

	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fatalErr{diskErr(err)}
	}
	h := sha256.New()
	d.setCounted(i, 0)
	drop := func() {
		f.Close()
		_ = os.Remove(part)
		d.setCounted(i, 0)
	}
	buf := make([]byte, 256<<10)
	var written int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(cdnStallTimeout)
			if written+int64(n) > fe.Size {
				drop()
				return hashFail{fe.Path}
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				drop()
				return fatalErr{diskErr(werr)}
			}
			h.Write(buf[:n])
			written += int64(n)
			d.addCounted(i, int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			drop()
			return d.pauseAware(parent, rerr)
		}
	}
	if written != fe.Size || hex.EncodeToString(h.Sum(nil)) != fe.SHA256 {
		drop()
		return hashFail{fe.Path}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(part)
		d.setCounted(i, 0)
		return fatalErr{diskErr(err)}
	}
	_ = os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return fatalErr{fmt.Errorf("не вдалося зберегти %s: %w", fe.Path, err)}
	}
	d.setCounted(i, fe.Size)
	d.markDone(i)
	d.mu.Lock()
	d.cdnFiles++
	d.cdnBytes += fe.Size
	d.mu.Unlock()
	return nil
}
