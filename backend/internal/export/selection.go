package export

import (
	"strings"
)

// ── Фільтри включення/виключення (порт Modrinth App ExportSelection) ────
//
// Користувач обирає в UI, які папки/файли збірки експортувати. Вибір
// надсилається списками include (вибрані шляхи) та exclude (вручну зняті
// прапорці) — аналог Modrinth App export_mrpack.rs: included_paths /
// excluded_paths, з яких будується дерево правил, а кожен файл/тека
// перевіряється найглибшим правилом, що його покриває.

// exportSelection — дерево правил вибору (шлях → включити/виключити).
// Найглибше правило має пріоритет: включив батька, але виключив папку —
// папка (і все в ній) не експортується.
type exportSelection struct {
	include map[string]bool // шлях (Unix '/') → вибраний
}

// PathFilter — публічна обгортка над деревом правил вибору: потрібна й
// mrpack-експорту, і .shaurmpack (ExportOptions.IncludePaths/ExcludePaths).
// Без неї два формати розуміли б «що я вибрав» по-різному — саме через це
// .shaurmpack раніше пакував УСЮ теку збірки, навіть коли в UI було
// вибрано лише mods.
//
// Немає жодного правила → Includes завжди true (повна збірка; так поводяться
// виклики без вибору, напр. експорт цілої теки у бандл).
type PathFilter struct {
	sel *exportSelection
}

// NewPathFilter будує фільтр за списками вибраних (included) і знятих
// (excluded) шляхів. Порожні списки → фільтр нічого не виключає.
func NewPathFilter(included, excluded []string) *PathFilter {
	if len(included) == 0 && len(excluded) == 0 {
		return &PathFilter{}
	}
	return &PathFilter{sel: newExportSelection(included, excluded)}
}

// Includes — чи потрапляє шлях (файл) в експорт.
func (f *PathFilter) Includes(path string) bool {
	if f == nil || f.sel == nil {
		return true
	}
	return f.sel.included(path)
}

// newExportSelection будує дерево правил із списків включення/виключення.
func newExportSelection(included, excluded []string) *exportSelection {
	m := map[string]bool{}
	for _, p := range included {
		p = normPath(p)
		if p != "" {
			m[p] = true
		}
	}
	for _, p := range excluded {
		p = normPath(p)
		if p != "" {
			m[p] = false
		}
	}
	return &exportSelection{include: m}
}

// included визначає, чи шлях (файл) має потрапити в експорт.
// Логіка — як ExportSelection::is_included: йдемо від кореня вглиб,
// найглибше правило перемагає; без правил — виключено.
func (s *exportSelection) included(path string) bool {
	path = normPath(path)
	if path == "" {
		return false
	}
	sel := false
	parts := strings.Split(path, "/")
	cur := ""
	for _, part := range parts {
		if cur == "" {
			cur = part
		} else {
			cur = cur + "/" + part
		}
		if v, ok := s.include[cur]; ok {
			sel = v
		}
	}
	// Якщо жодне правило не торкнулось шляху — успадковуємо батьківське
	// рішення (воно вже враховане в sel через цикл); якщо правил немає
	// зовсім — виключено.
	return sel
}

// normPath нормалізує відносний шлях: зворотні слеші → '/', прибирає
// початкові '/' і «./», порожній рядок = корінь.
func normPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "./")
	return p
}
