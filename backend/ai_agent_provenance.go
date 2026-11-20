package main

// ai_agent_provenance.go — реєстр «що створив ШІ-агент»: знімки світів збірок,
// бекапи світів серверів, збірки. Лежить окремим файлом (ai-provenance.json у
// теці даних), тож формат самих бекапів/збірок не змінюється. UI показує
// позначку «ШІ» біля копій, створених агентом.

import (
	"path/filepath"
	"sync"
	"time"

	"encoding/json"
	"os"
	"shaurma-launcher-backend/internal/atomicfile"
)

type aiProvEntry struct {
	By string `json:"by"` // "ai-agent" або назва MCP-клієнта
	At string `json:"at"`
}

var aiProv struct {
	mu     sync.Mutex
	loaded bool
	m      map[string]aiProvEntry
}

func (a *App) aiProvPath() string { return filepath.Join(a.cfg.Dir(), "ai-provenance.json") }

func (a *App) aiProvLoad() {
	if aiProv.loaded {
		return
	}
	aiProv.loaded = true
	aiProv.m = map[string]aiProvEntry{}
	if b, err := os.ReadFile(a.aiProvPath()); err == nil {
		_ = json.Unmarshal(b, &aiProv.m)
	}
}

// aiProvenanceMark записує, що ref (kind|id) створено агентом.
func (a *App) aiProvenanceMark(kind, ref string) {
	by := "ai-agent"
	aiAgent.actMu.Lock()
	if aiAgent.client != "" {
		by = aiAgent.client
	}
	aiAgent.actMu.Unlock()
	aiProv.mu.Lock()
	defer aiProv.mu.Unlock()
	a.aiProvLoad()
	aiProv.m[kind+"|"+ref] = aiProvEntry{By: by, At: time.Now().Format(time.RFC3339)}
	_ = atomicfile.WriteJSONAtomic(a.aiProvPath(), aiProv.m)
}

// aiProvenanceOf повертає автора запису ("" якщо створено не агентом).
func (a *App) aiProvenanceOf(kind, ref string) string {
	if a == nil || a.cfg == nil {
		return ""
	}
	aiProv.mu.Lock()
	defer aiProv.mu.Unlock()
	a.aiProvLoad()
	return aiProv.m[kind+"|"+ref].By
}

// aiProvenanceForget прибирає запис (після видалення створеного агентом).
func (a *App) aiProvenanceForget(kind, ref string) {
	aiProv.mu.Lock()
	defer aiProv.mu.Unlock()
	a.aiProvLoad()
	delete(aiProv.m, kind+"|"+ref)
	_ = atomicfile.WriteJSONAtomic(a.aiProvPath(), aiProv.m)
}

// GetAIAgentBuildAuthor — для значка «ШІ» у шапці збірки: порожньо, якщо збірку створив не агент.
func (a *App) GetAIAgentBuildAuthor(packID string) string { return a.aiProvenanceOf("build", packID) }
