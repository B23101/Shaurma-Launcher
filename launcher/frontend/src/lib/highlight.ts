// ── Легка підсвітка синтаксису для вбудованого перегляду файлів ────────
// Без зовнішніх залежностей: рядковий токенізатор за правилами мови, що
// повертає HTML-рядок із <span class="hl-*"> (кольори — у CSS компонента).
// Підтримує типові для Minecraft конфіги: JSON, properties/toml, yaml,
// java, js/ts, html/xml, css, python, mcfunction, логи.

interface Rule {
  re: RegExp
  cls: string
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// Рядковий токенізатор із липкими (sticky) RegExp: на кожній позиції
// пробує всі правила мови і бере найдовший збіг. Якщо жодне правило не
// підходить — рухаємось на ОДИН символ уперед, а не кидаємо решту рядка
// без підсвітки (інакше перша ж пробіл/дужка на початку рядка вбивала б
// всю підсвітку рядка, бо рядки рідко починаються одразу токеном).
function tokenizeLine(line: string, rules: Rule[]): string {
  let out = ''
  let pos = 0
  const len = line.length
  while (pos < len) {
    let best: { end: number; cls: string } | null = null
    for (const r of rules) {
      r.re.lastIndex = pos
      const m = r.re.exec(line)
      // Липкий флаг: збіг можливий лише точно з позиції pos.
      if (!m || m.index !== pos) continue
      const end = m.index + m[0].length
      if (end === m.index) continue // порожній збіг — пропускаємо
      const cand = { end, cls: r.cls }
      if (!best || end - pos > best.end - pos) best = cand
    }
    if (!best) {
      out += escapeHtml(line.charAt(pos))
      pos += 1
      continue
    }
    out += `<span class="${best.cls}">${escapeHtml(line.slice(pos, best.end))}</span>`
    pos = best.end
  }
  return out
}

const STR_DQ = { re: /"(?:\\.|[^"\\])*"/y, cls: 'hl-s' }
const STR_SQ = { re: /'(?:\\.|[^'\\])*'/y, cls: 'hl-s' }
const STR_BT = { re: /`(?:\\.|[^`\\])*`/y, cls: 'hl-s' }
const NUM = { re: /-?\b\d[\d_]*(?:\.\d+)?(?:[eE][+-]?\d+)?\b/y, cls: 'hl-n' }
const BOOL = { re: /\b(?:true|false|null|undefined)\b/y, cls: 'hl-b' }

const JSON_RULES: Rule[] = [
  { re: /"(?:\\.|[^"\\])*"(?=\s*:)/y, cls: 'hl-p' },
  STR_DQ,
  NUM,
  { re: /\b(?:true|false|null)\b/y, cls: 'hl-b' },
]

const PROPS_RULES: Rule[] = [
  { re: /^[#;].*/y, cls: 'hl-c' },
  { re: /^\[[^\]]*\]/y, cls: 'hl-a' },
  { re: /^[\w.\-/]+\s*(?=[=:])/y, cls: 'hl-p' },
  STR_DQ,
  STR_SQ,
  NUM,
  BOOL,
]

const YAML_RULES: Rule[] = [
  { re: /^#.*/y, cls: 'hl-c' },
  { re: /^[\w][\w-]*(?=\s*:)/y, cls: 'hl-p' },
  STR_DQ,
  STR_SQ,
  NUM,
  BOOL,
]

const JAVA_KW = 'public|private|protected|static|final|class|interface|extends|implements|return|void|new|if|else|for|while|switch|case|break|continue|import|package|try|catch|throw|throws|this|super|boolean|int|float|double|long|byte|short|char|var|record|default|null|true|false|synchronized|volatile|transient'
const JAVA_RULES: Rule[] = [
  { re: /\/\/.*$/y, cls: 'hl-c' },
  { re: /\/\*[\s\S]*?\*\//y, cls: 'hl-c' },
  { re: /@\w+/y, cls: 'hl-a' },
  { re: new RegExp('\\b(?:' + JAVA_KW + ')\\b', 'y'), cls: 'hl-k' },
  { re: /\b\w+(?=\s*\()/y, cls: 'hl-f' },
  STR_DQ,
  NUM,
]

const JS_KW = 'function|const|let|var|return|if|else|for|while|new|class|import|export|from|default|async|await|try|catch|throw|switch|case|break|continue|typeof|instanceof|this|null|undefined|true|false|interface|type|enum|extends|implements|public|private|static|readonly|keyof|in|of|yield|do|delete'
const JS_RULES: Rule[] = [
  { re: /\/\/.*$/y, cls: 'hl-c' },
  { re: /\/\*[\s\S]*?\*\//y, cls: 'hl-c' },
  { re: /@\w+/y, cls: 'hl-a' },
  { re: new RegExp('\\b(?:' + JS_KW + ')\\b', 'y'), cls: 'hl-k' },
  { re: /\b\w+(?=\s*\()/y, cls: 'hl-f' },
  STR_DQ,
  STR_SQ,
  STR_BT,
  NUM,
]

const HTML_RULES: Rule[] = [
  { re: /<!--[\s\S]*?-->/y, cls: 'hl-c' },
  { re: /<\/?[\w:-]+/y, cls: 'hl-t' },
  { re: /\b[\w-]+(?==)/y, cls: 'hl-a' },
  STR_DQ,
  STR_SQ,
]

const CSS_RULES: Rule[] = [
  { re: /\/\*[\s\S]*?\*\//y, cls: 'hl-c' },
  { re: /@\w+/y, cls: 'hl-a' },
  { re: /#[\da-fA-F]{3,8}\b/y, cls: 'hl-n' },
  { re: /[\w-]+(?=\s*:)/y, cls: 'hl-p' },
  STR_DQ,
  NUM,
]

const PY_KW = 'def|class|return|if|elif|else|for|while|import|from|as|with|try|except|finally|raise|yield|lambda|pass|break|continue|None|True|False|and|or|not|in|is|global|nonlocal|del|assert|async|await|match|case'
const PY_RULES: Rule[] = [
  { re: /#.*$/y, cls: 'hl-c' },
  { re: new RegExp('\\b(?:' + PY_KW + ')\\b', 'y'), cls: 'hl-k' },
  { re: /\b\w+(?=\s*\()/y, cls: 'hl-f' },
  STR_DQ,
  STR_SQ,
  NUM,
]

const MCFN_RULES: Rule[] = [
  { re: /^#.*$/y, cls: 'hl-c' },
  { re: /@[apres]/y, cls: 'hl-a' },
  { re: /^\s*[\w:.-]+(?=\s)/y, cls: 'hl-p' },
  STR_DQ,
  STR_SQ,
  NUM,
]

const LOG_RULES: Rule[] = [
  { re: /\b(?:WARN|WARNING|ERROR|FATAL|SEVERE|DEBUG|TRACE|INFO|Exception|Caused by|at)\b/y, cls: 'hl-k' },
  { re: /\d{2}:\d{2}:\d{2}(?:[.,]\d+)?/y, cls: 'hl-n' },
  STR_DQ,
  STR_SQ,
]

const RULES: Record<string, Rule[]> = {
  json: JSON_RULES,
  properties: PROPS_RULES,
  yaml: YAML_RULES,
  java: JAVA_RULES,
  js: JS_RULES,
  html: HTML_RULES,
  css: CSS_RULES,
  python: PY_RULES,
  mcfunction: MCFN_RULES,
  log: LOG_RULES,
}

function langFor(name: string): string {
  const ext = name.toLowerCase().split('.').pop() ?? ''
  if (ext === 'json' || ext === 'mcmeta') return 'json'
  if (ext === 'properties' || ext === 'options' || ext === 'cfg' || ext === 'toml' || ext === 'lang') return 'properties'
  if (ext === 'yaml' || ext === 'yml') return 'yaml'
  if (ext === 'java') return 'java'
  if (ext === 'js' || ext === 'mjs' || ext === 'cjs' || ext === 'ts' || ext === 'jsx' || ext === 'tsx') return 'js'
  if (ext === 'html' || ext === 'xml' || ext === 'svg' || ext === 'mcmeta') return 'html'
  if (ext === 'css' || ext === 'scss' || ext === 'less') return 'css'
  if (ext === 'py' || ext === 'pyw') return 'python'
  if (ext === 'mcfunction') return 'mcfunction'
  if (ext === 'log') return 'log'
  return ''
}

/** Повертає HTML з <span class="hl-*"> для підсвітки вмісту файлу. */
export function highlightCode(name: string, code: string): string {
  const lang = langFor(name)
  if (!lang) return escapeHtml(code)
  const rules = RULES[lang]
  const lines = code.split('\n')
  return lines.map((l) => tokenizeLine(l, rules)).join('\n')
}

/** Короткий ярлик мови для шапки перегляду файлу ('' = невідомо). */
export function langLabel(name: string): string {
  const lang = langFor(name)
  return lang.toUpperCase()
}

// ── Перевірка синтаксису ────────────────────────────────────────────────
// Повний парсер для кожної мови не тягнемо: робимо строгий парсинг для
// JSON (найчастіший формат конфігів Minecraft). Для решти мов валидації
// немає — повертаємо ok. Позиція помилки витягується з повідомлення
// V8 ("... position N") і переводиться в рядок/колонку.

export interface SyntaxResult {
  ok: boolean
  checked: boolean
  message?: string
  line?: number
  col?: number
}

export function validateSyntax(name: string, code: string): SyntaxResult {
  if (langFor(name) !== 'json') return { ok: true, checked: false }
  if (!code || code.trim() === '') return { ok: true, checked: true }
  try {
    JSON.parse(code)
    return { ok: true, checked: true }
  } catch (e: any) {
    const raw = String(e?.message ?? e)
    const m = raw.match(/position (\d+)/)
    let line: number | undefined
    let col: number | undefined
    if (m) {
      const pos = parseInt(m[1], 10)
      const head = code.slice(0, pos)
      line = head.split('\n').length
      const lastNl = head.lastIndexOf('\n')
      col = lastNl < 0 ? pos + 1 : pos - lastNl
    }
    return { ok: false, checked: true, message: raw, line, col }
  }
}
