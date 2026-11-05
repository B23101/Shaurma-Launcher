package custompack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// cfBatchChunk — скільки fileIds шлемо в одному POST /v1/mods/files.
// CurseForge приймає великі списки, але менші пачки швидше відповідають
// і не ризикують упертися в ліміт розміру запиту.
const cfBatchChunk = 200

type cfBatchFilesResponse struct {
	Data []struct {
		ID           int      `json:"id"`
		ModID        int      `json:"modId"`
		FileName     string   `json:"fileName"`
		DownloadURL  string   `json:"downloadUrl"`
		FileLength   int64    `json:"fileLength"`
		GameVersions []string `json:"gameVersions"`
		Hashes       []struct {
			Value string `json:"value"`
			Algo  int    `json:"algo"`
		} `json:"hashes"`
	} `json:"data"`
}

// GetModFilesBatchCtx отримує метадані багатьох файлів ОДНИМ запитом на
// пачку (POST /v1/mods/files) замість окремого GET на кожен мод. На
// модпаку з 300 модами при ліміті cfRatePerSec це 2 запити замість 300
// (~75 с очікування лише на резолв). Файли без downloadUrl повертаються
// з порожнім DownloadURL — викликач сам добирає посилання або позначає
// їх заблокованими. Файлів, яких немає у відповіді, у мапі немає.
func (c *CurseForgeClient) GetModFilesBatchCtx(ctx context.Context, fileIDs []int, onChunk func(done int)) (map[int]*CFFileInfo, error) {
	out := make(map[int]*CFFileInfo, len(fileIDs))
	for start := 0; start < len(fileIDs); start += cfBatchChunk {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		end := start + cfBatchChunk
		if end > len(fileIDs) {
			end = len(fileIDs)
		}
		payload, err := json.Marshal(map[string][]int{"fileIds": fileIDs[start:end]})
		if err != nil {
			return out, err
		}
		req, err := http.NewRequest(http.MethodPost, curseForgeAPIBase+"/mods/files", bytes.NewReader(payload))
		if err != nil {
			return out, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.doRequestCtx(ctx, req)
		if err != nil {
			return out, err
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if rerr != nil {
			return out, rerr
		}
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("CurseForge API: HTTP %d (batch files)", resp.StatusCode)
		}
		var parsed cfBatchFilesResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return out, err
		}
		for _, f := range parsed.Data {
			info := &CFFileInfo{
				FileName:     f.FileName,
				DownloadURL:  f.DownloadURL,
				FileLength:   f.FileLength,
				GameVersions: f.GameVersions,
				ModLoaders:   cfLoaderNamesFrom(f.GameVersions),
			}
			for _, h := range f.Hashes {
				if h.Algo == 1 {
					info.SHA1 = h.Value
				}
			}
			out[f.ID] = info
		}
		if onChunk != nil {
			onChunk(end)
		}
	}
	return out, nil
}
