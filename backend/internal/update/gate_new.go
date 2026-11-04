package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Джерела нової системи (ті самі, що в оновлювачі: newupdate.DefaultSignalURL/Repo).
var (
	signalURL  = "https://api.shaurma-launcher.org"
	githubRepo = "B23101/Shaurma-Launcher"
)

// checkNew — версія з сигналу нового worker, якщо він мовчить — з маніфесту
// останнього релізу GitHub. Підпис тут не перевіряється: це лише напис
// «доступна версія X», встановлення робить updater з усіма перевірками.
func (c *Checker) checkNew() (*UpdateInfo, error) {
	ver := ""
	q := url.Values{"channel": {c.channel}, "current": {c.currentVer}}
	var sig struct {
		Version string `json:"version"`
	}
	if err := c.getJSON(signalURL+"/v1/update/check?"+q.Encode(), &sig); err == nil {
		ver = sig.Version
	} else {
		var m struct {
			Version string `json:"version"`
		}
		if err := c.getJSON("https://github.com/"+githubRepo+"/releases/latest/download/launcher-manifest.json", &m); err != nil {
			return nil, fmt.Errorf("update: сервер оновлень і GitHub недоступні: %w", err)
		}
		ver = m.Version
	}
	return &UpdateInfo{Available: ver != "" && Compare(ver, c.currentVer) > 0, Version: ver}, nil
}

func (c *Checker) getJSON(u string, out any) error {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
