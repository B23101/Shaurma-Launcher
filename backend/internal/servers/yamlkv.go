// yamlkv.go — точкове читання/запис скалярів у блочному YAML (конфіги
// Paper: config/paper-global.yml, paper.yml) за шляхом ключів.
//
// Повний YAML-парсер тут не потрібен: Paper пише конфіги у простому
// блочному стилі з відступами пробілами. Раніше glue просто дописував
// «proxies: velocity: …» у кінець файлу і пропускав файл, якщо в ньому
// вже був рядок «secret: » — а Paper після першого старту сам пише
// «secret: ”» з enabled: false, тож forwarding так і лишався вимкненим.
// Тепер потрібні ключі ставляться на своє місце, решта файлу не змінюється.
package servers

import "strings"

type yamlLine struct {
	indent int
	key    string // "" — порожній рядок / коментар / елемент списку
	value  string // значення після «key:» (може бути порожнім)
}

func parseYAMLLine(s string) yamlLine {
	trimmed := strings.TrimLeft(s, " ")
	l := yamlLine{indent: len(s) - len(trimmed)}
	t := strings.TrimSpace(trimmed)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "-") {
		return yamlLine{indent: -1}
	}
	i := strings.Index(t, ":")
	if i <= 0 {
		return yamlLine{indent: -1}
	}
	l.key = strings.Trim(t[:i], `"'`)
	l.value = strings.TrimSpace(t[i+1:])
	return l
}

// yamlFind шукає ключ серед прямих дітей блоку lines[lo:hi] з відступом
// батька parentIndent. Повертає індекс рядка ключа, кінець його блоку і
// відступ дітей блоку (для вставки нових ключів).
func yamlFind(lines []string, lo, hi, parentIndent int, key string) (idx, end, childIndent int) {
	childIndent = -1
	idx = -1
	for i := lo; i < hi; i++ {
		l := parseYAMLLine(lines[i])
		if l.indent < 0 || l.indent <= parentIndent {
			continue
		}
		if childIndent < 0 {
			childIndent = l.indent
		}
		if l.indent == childIndent && l.key == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		return -1, hi, childIndent
	}
	end = hi
	for j := idx + 1; j < hi; j++ {
		l := parseYAMLLine(lines[j])
		if l.indent >= 0 && l.indent <= childIndent {
			end = j
			break
		}
	}
	return idx, end, childIndent
}

// yamlBlockEnd — кінець блоку батька: останній рядок, що належить йому
// (без хвостових порожніх рядків — нові ключі стають поруч із сусідами).
func yamlBlockEnd(lines []string, lo, hi int) int {
	end := lo
	for i := lo; i < hi; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			end = i + 1
		}
	}
	return end
}

// getYAMLScalar повертає значення за шляхом (без лапок) і чи знайдено ключ.
func getYAMLScalar(doc string, path []string) (string, bool) {
	lines := strings.Split(doc, "\n")
	lo, hi, parent := 0, len(lines), -1
	for depth, key := range path {
		idx, end, ci := yamlFind(lines, lo, hi, parent, key)
		if idx < 0 {
			return "", false
		}
		if depth == len(path)-1 {
			return strings.Trim(parseYAMLLine(lines[idx]).value, `"'`), true
		}
		lo, hi, parent = idx+1, end, ci
	}
	return "", false
}

// setYAMLScalar ставить value (уже у YAML-вигляді: true, 'текст', 42) за
// шляхом path, створюючи відсутні проміжні мапи. Решта документа лишається
// як була.
func setYAMLScalar(doc string, path []string, value string) string {
	lines := strings.Split(doc, "\n")
	lo, hi, parent := 0, len(lines), -1
	for depth, key := range path {
		idx, end, ci := yamlFind(lines, lo, hi, parent, key)
		indent := ci
		if indent < 0 {
			indent = parent + 2
			if parent < 0 {
				indent = 0
			}
		}
		if idx < 0 {
			// Вставляємо решту шляху в кінець блоку батька.
			at := yamlBlockEnd(lines, lo, hi)
			if parent < 0 {
				at = yamlBlockEnd(lines, 0, len(lines))
			}
			var add []string
			for d, k := range path[depth:] {
				pad := strings.Repeat(" ", indent+2*d)
				if depth+d == len(path)-1 {
					add = append(add, pad+k+": "+value)
				} else {
					add = append(add, pad+k+":")
				}
			}
			lines = append(lines[:at], append(add, lines[at:]...)...)
			return strings.Join(lines, "\n")
		}
		pad := strings.Repeat(" ", indent)
		if depth == len(path)-1 {
			lines[idx] = pad + key + ": " + value
			return strings.Join(lines, "\n")
		}
		// Проміжний ключ з інлайн-значенням (напр. «velocity: {}») стає мапою.
		if parseYAMLLine(lines[idx]).value != "" {
			lines[idx] = pad + key + ":"
		}
		lo, hi, parent = idx+1, end, indent
	}
	return strings.Join(lines, "\n")
}
