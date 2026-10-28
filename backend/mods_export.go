package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"shaurma-launcher-backend/internal/model"
)

// ── Експорт списку модів у різних форматах ─────────────────────────────
// Порт Prism ExportToModList (modplatform/helpers/ExportToModList.cpp):
// список модів збірки у форматі html / markdown / plain / json / csv, з
// опційними полями (автор, URL, версія, ім'я файлу). Чисто зручність
// користувача: скопіювати список модів для форуму/README/спільноти.

// ModListExportOptions — налаштування експорту списку модів.
type ModListExportOptions struct {
	// Format: "html" | "markdown" | "plain" | "json" | "csv".
	Format string `json:"format"`
	// Додаткові поля у кожному рядку (як OptionalData у Prism).
	Authors  bool `json:"authors"`
	Url      bool `json:"url"`
	Version  bool `json:"version"`
	FileName bool `json:"fileName"`
	// OnlyEnabled — експортувати лише УВІМКНЕНІ моди (вимкнені (.disabled)
	// не потрапляють у список — як і в Prism, де список модів показує
	// активні).
	OnlyEnabled bool `json:"onlyEnabled"`
	// FileNames — обмеження експорту лише цими файлами модів (порожній =
	// всі). Використовується кнопкою «Експортувати вибране» у панелі
	// масових дій менеджера модів: список будується з обраних чекбоксами.
	FileNames []string `json:"fileNames,omitempty"`
}

// modRow — один мод, підготовлений до експорту (спільна форма для всіх
// форматів).
type modRow struct {
	name     string
	version  string
	authors  string
	fileName string
	url      string
}

// ExportModList повертає список модів збірки у вказаному форматі.
// Моди беруться тим самим сканером, що й вкладка «Моди» (ScanModsFull) —
// без мережевих запитів (версії/автори читаються з jar-метаданих і
// локального кешу).
//
// Для посилань на моди (як у Prism ExportToModList) додатково зливається
// закешований статус оновлень: Source/ProjectID/ProjectSlug живуть саме в
// ньому (сканер з диска їх не знає). Якщо перевірка оновлень ще не
// виконувалась — посилань немає, але це чесно відображає відоме лаунчеру.
func (a *App) ExportModList(packID string, opts ModListExportOptions) (string, error) {
	mods := a.ScanModsFull(packID)
	if mods == nil {
		mods = []model.ModEntry{}
	}
	// mcVersion/loader збірки — для злиття закешованої інфи про джерело
	// мода (Modrinth/CurseForge), щоб у списку були реальні посилання.
	mcVersion, loader := a.modTargetEnv(packID)
	if mcVersion != "" && loader != "" {
		mods = a.mergeCachedUpdateInfo(packID, mcVersion, loader, mods)
	}

	return exportModListFromMods(mods, opts)
}

// exportModListFromMods перетворює проскановані моди у рядки й рендерить
// обраний формат (html/markdown/plain/json/csv).
func exportModListFromMods(mods []model.ModEntry, opts ModListExportOptions) (string, error) {
	rows := make([]modRow, 0, len(mods))
	// Фільтр «тільки вибрані»: якщо передано fileNames — експортуємо ЛИШЕ
	// ці файли. Порядок рядків — як у переданому списку (тобто порядок
	// вибору юзера), а не алфавітний порядок сканера.
	selected := make(map[string]bool, len(opts.FileNames))
	for _, f := range opts.FileNames {
		if f != "" {
			selected[f] = true
		}
	}
	byFile := make(map[string]model.ModEntry, len(mods))
	for _, m := range mods {
		byFile[m.FileName] = m
	}
	emitRow := func(m model.ModEntry) {
		name := m.Name
		if name == "" {
			name = m.FileName
		}
		r := modRow{name: name, version: m.Version, fileName: m.FileName, authors: m.Author}
		if opts.Url {
			r.url = modPageURL(m.Source, m.ProjectSlug, m.ProjectID)
		}
		rows = append(rows, r)
	}
	if len(selected) > 0 {
		for _, f := range opts.FileNames {
			if m, ok := byFile[f]; ok {
				emitRow(m)
			}
		}
	} else {
		for _, m := range mods {
			if opts.OnlyEnabled && !m.Enabled {
				continue
			}
			emitRow(m)
		}
	}

	switch strings.ToLower(opts.Format) {
	case "html":
		return exportModsHTML(rows, opts), nil
	case "markdown", "md":
		return exportModsMarkdown(rows, opts), nil
	case "json":
		return exportModsJSON(rows, opts)
	case "csv":
		return exportModsCSV(rows, opts)
	default:
		return exportModsPlain(rows, opts), nil
	}
}

// formatModRow збирає рядок "Name [Version] by Author (file)" з опціями.
// htmlEscaped=true — для HTML (у Prism — toHtmlEscaped); для markdown —
// URL стає лінком [Name](url), для plain — "(url)"; обробляється в
// кожному форматі окремо, тут лише спільна збірка частин.
func formatModRow(r modRow, opts ModListExportOptions, htmlEscaped bool) string {
	name := r.name
	if htmlEscaped {
		name = html.EscapeString(name)
	}
	if opts.Url && r.url != "" {
		if htmlEscaped {
			name = fmt.Sprintf("<a href=\"%s\">%s</a>", html.EscapeString(r.url), name)
		} else {
			name = fmt.Sprintf("[%s](%s)", name, r.url)
		}
	}
	line := name
	if opts.Version && r.version != "" {
		v := r.version
		if htmlEscaped {
			v = html.EscapeString(v)
		}
		line += " [" + v + "]"
	}
	if opts.Authors && r.authors != "" {
		a := r.authors
		if htmlEscaped {
			a = html.EscapeString(a)
		}
		line += " by " + a
	}
	if opts.FileName && r.fileName != "" {
		f := r.fileName
		if htmlEscaped {
			f = html.EscapeString(f)
		}
		line += " (" + f + ")"
	}
	return line
}

// ── Формати ────────────────────────────────────────────────────────────

func exportModsHTML(rows []modRow, opts ModListExportOptions) string {
	var b strings.Builder
	b.WriteString("<html><body><ul>\n")
	for _, r := range rows {
		b.WriteString("\t<li>" + formatModRow(r, opts, true) + "</li>\n")
	}
	b.WriteString("</ul></body></html>")
	return b.String()
}

func exportModsMarkdown(rows []modRow, opts ModListExportOptions) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString("- " + formatModRow(r, opts, false) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func exportModsPlain(rows []modRow, opts ModListExportOptions) string {
	var b strings.Builder
	for _, r := range rows {
		line := r.name
		if opts.Url && r.url != "" {
			line += " (" + r.url + ")"
		}
		if opts.Version && r.version != "" {
			line += " [" + r.version + "]"
		}
		if opts.Authors && r.authors != "" {
			line += " by " + r.authors
		}
		if opts.FileName && r.fileName != "" {
			line += " (" + r.fileName + ")"
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func exportModsJSON(rows []modRow, opts ModListExportOptions) (string, error) {
	type entry struct {
		Name     string `json:"name"`
		Version  string `json:"version,omitempty"`
		Author   string `json:"author,omitempty"`
		FileName string `json:"fileName,omitempty"`
		URL      string `json:"url,omitempty"`
	}
	out := make([]entry, 0, len(rows))
	for _, r := range rows {
		e := entry{Name: r.name}
		if opts.Version && r.version != "" {
			e.Version = r.version
		}
		if opts.Authors && r.authors != "" {
			e.Author = r.authors
		}
		if opts.FileName && r.fileName != "" {
			e.FileName = r.fileName
		}
		if opts.Url && r.url != "" {
			e.URL = r.url
		}
		out = append(out, e)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func exportModsCSV(rows []modRow, opts ModListExportOptions) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	header := []string{"name"}
	if opts.Version {
		header = append(header, "version")
	}
	if opts.Authors {
		header = append(header, "author")
	}
	if opts.FileName {
		header = append(header, "fileName")
	}
	if opts.Url {
		header = append(header, "url")
	}
	if err := w.Write(header); err != nil {
		return "", err
	}
	for _, r := range rows {
		rec := []string{r.name}
		if opts.Version {
			rec = append(rec, r.version)
		}
		if opts.Authors {
			rec = append(rec, r.authors)
		}
		if opts.FileName {
			rec = append(rec, r.fileName)
		}
		if opts.Url {
			rec = append(rec, r.url)
		}
		if err := w.Write(rec); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return b.String(), nil
}
