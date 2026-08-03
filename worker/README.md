# Shaurma Worker — інструкція деплою

## 1. Встановити Wrangler

```bash
npm install -g wrangler
wrangler login
```

## 2. Замінити bucket name у wrangler.toml

Відкрий `wrangler.toml`, знайди `bucket_name = "shaurm-packs"` — заміни на реальну назву.

## 3. Додати секрети

```bash
wrangler secret put TOKEN_SHAURMA
# → вводиш токен для лаунчера зі збірками (мінімум 32 символи, рандомний)

wrangler secret put TOKEN_BASE
# → вводиш токен для базового лаунчера (інший рандомний рядок)
```

Генератор токенів (PowerShell):
```powershell
-join ((65..90)+(97..122)+(48..57) | Get-Random -Count 40 | %{[char]$_})
```

## 4. Деплой

```bash
wrangler deploy
```

Отримаєш URL виду: `https://shaurma-proxy.YOUR_NAME.workers.dev`

## 5. Структура R2 яку треба підготувати

```
bucket/
├── launchers/
│   ├── shaurma/
│   │   ├── launcher-version.json     ← завантажити з папки r2-structure/
│   │   └── ShaurmLauncher-shaurma-2.2.0.exe   ← твій скомпільований exe
│   └── base/
│       ├── launcher-version.json     ← завантажити з папки r2-structure/
│       └── ShaurmLauncher-base-2.2.0.exe      ← твій скомпільований exe
├── server-manifest.json              ← вже є (Pack Manager оновлює)
├── index.json                        ← вже є
└── packs/                            ← вже є
```

launcher-version.json завантажити через Pack Manager або через wrangler:
```bash
wrangler r2 object put shaurm-packs/launchers/shaurma/launcher-version.json \
  --file ./r2-structure/launchers/shaurma/launcher-version.json \
  --content-type application/json

wrangler r2 object put shaurm-packs/launchers/base/launcher-version.json \
  --file ./r2-structure/launchers/base/launcher-version.json \
  --content-type application/json
```

## 6. WAF Rate Limiting (Cloudflare Dashboard)

Security → WAF → Rate Limiting Rules → Create:

**Rule 1 — ліміт по IP:**
- Expression: `http.host eq "shaurma-proxy.YOUR_NAME.workers.dev"`
- Rate: 100 requests / 1 minute / IP
- Action: Block (10 хвилин)

**Rule 2 — ліміт по токену (захист від distributed атаки):**
- Expression: `http.request.headers["x-shaurma-token"][0] ne ""`
- Rate: 300 requests / 1 minute / token value
- Action: Block (10 хвилин)

## 7. Значення токенів в лаунчерах

Шаурма лаунчер (launcher-shaurma/):
```
CDN_TOKEN = <твій TOKEN_SHAURMA>
```

Базовий лаунчер (launcher-base/):
```
CDN_TOKEN = <твій TOKEN_BASE>
```
