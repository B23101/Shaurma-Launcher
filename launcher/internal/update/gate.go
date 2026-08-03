package update

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type Checker struct {
	client     *http.Client
	currentVer string
	updateURL  string
}

type UpdateInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version"`
	DownloadURL  string `json:"downloadUrl"`
	ChangelogURL string `json:"changelogUrl"`
	IsCritical   bool   `json:"isCritical"`
}

func NewChecker(currentVersion string) *Checker {
	return &Checker{
		client:     &http.Client{Timeout: 15 * time.Second},
		currentVer: currentVersion,
		updateURL:  "https://api.shaurma.lol/v1/update",
	}
}

func (c *Checker) Check() (*UpdateInfo, error) {
	resp, err := c.client.Get(c.updateURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Latest    string `json:"latest"`
		URL       string `json:"url"`
		Changelog string `json:"changelog"`
		Critical  bool   `json:"critical"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	available := result.Latest != "" && result.Latest != c.currentVer
	return &UpdateInfo{
		Available:    available,
		Version:      result.Latest,
		DownloadURL:  result.URL,
		ChangelogURL: result.Changelog,
		IsCritical:   result.Critical,
	}, nil
}

func (c *Checker) SetUpdateURL(url string) { c.updateURL = url }

func (c *Checker) CurrentVersion() string {
	if c.currentVer == "" {
		return "2.0.0"
	}
	return c.currentVer
}

func (c *Checker) SetVersion(v string) { c.currentVer = v }

func GetLatestVersion() string {
	return "2.0.0"
}

func CompareVersions(current, latest string) bool {
	if current == "" || latest == "" {
		return false
	}
	return current != latest
}
