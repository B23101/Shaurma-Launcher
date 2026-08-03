/**
 * Shaurma Launcher — Cloudflare Worker
 *
 * Секрети (Workers → Settings → Variables → Secrets):
 *   TOKEN_SHAURMA  — лаунчер зі збірками (повний доступ)
 *   TOKEN_BASE     — базовий лаунчер (тільки свій exe + launcher-version.json)
 *
 * Binding (Workers → Settings → Bindings → R2 Bucket):
 *   Variable name: R2_BUCKET → твій bucket
 *
 * Структура R2:
 *   launcher-version.json            ← SHAURMA версія (корінь, для зворотної сумісності)
 *   launchers/shaurma/launcher-version.json
 *   launchers/shaurma/*.exe
 *   launchers/base/launcher-version.json
 *   launchers/base/*.exe
 *   server-manifest.json
 *   index.json
 *   packs/**
 *   java/**
 *
 * Ендпоінти:
 *   GET/HEAD  <будь-який шлях>  — звичайна роздача одного файлу (підтримує Range)
 *   POST      /batch            — пакетна роздача декількох файлів в одній відповіді
 *                                 (див. розділ BATCH нижче). Економить ліміт запитів/день
 *                                 для великої кількості дрібних файлів (bundled-моди, архіви).
 */

const BATCH_MAX_FILES        = 64;     // максимум файлів в одному batch-запиті
const BATCH_MAX_BODY_BYTES   = 8192;   // максимальний розмір JSON-тіла запиту (список ключів)
const BATCH_MAX_TOTAL_BYTES  = 512 * 1024 * 1024; // 512 MB — максимальний СУМАРНИЙ розмір файлів в одному batch
// Одиничний файл більший за це — НЕ повинен потрапляти в /batch, навіть якщо
// сумарний розмір batch вкладається в BATCH_MAX_TOTAL_BYTES. Причина: стрім
// одного великого файлу всередині одного виклику Worker'а впирається у ліміт
// часу виконання (CPU/wall-clock) — Worker обриває виконання посередині файлу,
// і клієнт отримує обіцяний Content-Length, але фактичний потік, що обривається
// раніше (Unexpected EOF). Великі файли мають йти через /file (Range-запити),
// де кожен chunk — окремий, короткий Worker-виклик.
const BATCH_MAX_SINGLE_FILE_BYTES = 20 * 1024 * 1024; // 20 MB
const BATCH_MAGIC            = 0x53484252; // "SHBR" — Shaurma Batch Response, перші 4 байти відповіді
const MAX_CACHE_BODY_BYTES   = 10 * 1024 * 1024; // 10 MB — максимальний розмір тіла для кешування (Cache API)

export default {
  async fetch(request, env, ctx) {

    // ── 1. OPTIONS preflight ───────────────────────────────────────────────
    if (request.method === 'OPTIONS') {
      return new Response(null, { status: 204, headers: corsHeaders() });
    }

    const url = new URL(request.url);

    // ── 1б. GET /health — легкий health-check без токена і без R2-запиту ───
    // Потрібен лаунчеру (internal/sync), щоб явно розрізняти "немає
    // інтернету взагалі" (fetch кине мережеву помилку ще ДО того, як дійде
    // сюди) від "worker відповідає, але щось не так на рівні токена/R2" —
    // обидва випадки лаунчер показує користувачу по-різному.
    if (request.method === 'GET' && url.pathname === '/health') {
      return new Response(JSON.stringify({ ok: true, ts: Date.now() }), {
        status: 200,
        headers: {
          'Content-Type': 'application/json; charset=utf-8',
          'Cache-Control': 'no-cache, no-store',
          ...corsHeaders(),
        },
      });
    }

    // ── 2. POST /batch — пакетний ендпоінт (окрема гілка, своя валідація) ──
    if (request.method === 'POST' && url.pathname === '/batch') {
      return handleBatch(request, env);
    }

    // ── 3. Тільки GET та HEAD для звичайної роздачі ────────────────────────
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      return errResp(405, 'Method Not Allowed');
    }

    // ── 4. Захист від великих тіл (memory bomb) ────────────────────────────
    const cl = parseInt(request.headers.get('content-length') || '0');
    if (cl > 512) {
      return errResp(400, 'Bad Request');
    }

    // ── 5. User-Agent: лише ShaurmaLauncher ───────────────────────────────
    // Захист від curl/python-скриптів що намагаються підібрати токен.
    // Зловмисник може підробити UA, але це усуває 99% автоматизованих сканерів.
    const ua = request.headers.get('User-Agent') || '';
    if (!ua.startsWith('ShaurmaLauncher/')) {
      // Повертаємо 404, а не 403 — не розкриваємо що ендпоінт існує
      return errResp(404, 'Not Found');
    }

    // ── 6. Токен ───────────────────────────────────────────────────────────
    const token = (request.headers.get('X-Shaurma-Token') || '').trim();
    if (token.length < 16) {
      return errResp(401, 'Unauthorized');
    }

    // ── 7. Timing-safe порівняння токенів ─────────────────────────────────
    const tokenType = resolveTokenType(token, env);
    if (!tokenType) {
      // Штучна затримка 50–150ms — ускладнює timing-based brute-force
      await randomDelay(50, 150);
      return errResp(403, 'Forbidden');
    }

    // ── 8. Парсинг та валідація шляху ─────────────────────────────────────
    const rawPath   = url.pathname;
    const pathname  = tryDecodeURIComponent(rawPath);

    // Path traversal guard (на декодованому шляху — ловить %2e%2e)
    if (
      pathname.includes('..') ||
      pathname.includes('//') ||
      pathname.includes('\x00') ||
      pathname.includes('\\')
    ) {
      return errResp(400, 'Bad Request');
    }

    // ── 9. Маппінг URL → R2 key з перевіркою прав ─────────────────────────
    const r2Key = resolveR2Key(pathname, tokenType);
    if (r2Key === null) {
      return errResp(403, 'Forbidden');
    }

    // ── 10. Range header для resumable/паралельного download ──────────────
    const rangeHeader = request.headers.get('Range');
    const isRange     = !!rangeHeader;
    const r2Options   = {};
    if (rangeHeader) {
      const parsed = parseRange(rangeHeader);
      if (parsed) r2Options.range = parsed;
    }

    // ── 11. Cache API — кешування GET на краю мережі ──────────────────────
    if (!isRange && request.method === 'GET') {
      const cacheKey  = new Request(url.toString(), { method: 'GET' });
      const cached    = await caches.default.match(cacheKey);
      if (cached) return cached;
    }

    // ── 12. Отримуємо об'єкт з R2 ────────────────────────────────────────
    let object;
    try {
      object = await env.R2_BUCKET.get(r2Key, r2Options);
    } catch (err) {
      console.error('R2 get error:', err.message);
      return errResp(500, 'Internal Server Error');
    }

    if (!object) {
      return errResp(404, 'Not Found');
    }

    // ── 13. HEAD — тільки заголовки ──────────────────────────────────────
    if (request.method === 'HEAD') {
      return new Response(null, {
        status: 200,
        headers: buildHeaders(object, r2Key),
      });
    }

    // ── 14. GET — віддаємо тіло ──────────────────────────────────────────
    const status = isRange ? 206 : 200;
    const headers = buildHeaders(object, r2Key);

    // Для Range-відповідей object.size — це розмір ВСЬОГО об'єкта в R2,
    // а не довжина запитаного шматка. Якщо залишити Content-Length рівним
    // object.size, клієнт (OkHttp) чекає набагато більше байт, ніж реально
    // прийде в тілі — з'єднання "висить" до таймауту замість миттєвого EOF.
    // Тут перезаписуємо Content-Length на реальну довжину чанку і додаємо
    // Content-Range, як і належить для 206 Partial Content.
    if (isRange && r2Options.range) {
      const offset = r2Options.range.offset;
      const length = r2Options.range.length ?? (object.size - offset);
      headers.set('Content-Length', String(length));
      headers.set('Content-Range', `bytes ${offset}-${offset + length - 1}/${object.size}`);
    }

    const response = new Response(object.body, {
      status,
      headers,
    });

    // Кешуємо GET-відповіді для повторних запитів (тільки не Range, і тільки
    // файли помірного розміру — великі mrpack/java-runtime не кешуємо).
    if (!isRange && status === 200 && object.size <= MAX_CACHE_BODY_BYTES) {
      const cacheKey = new Request(url.toString(), { method: 'GET' });
      ctx.waitUntil(caches.default.put(cacheKey, response.clone()));
    }

    return response;
  },
};

// ═══════════════════════════════════════════════════════════════════════════
// BATCH — пакетна роздача декількох файлів в одній HTTP-відповіді
//
// Навіщо: при синхронізації збірки лаунчер може потребувати десятки
// bundled-модів/архівів одночасно. Кожен такий файл окремим запитом —
// це окрема одиниця денного ліміту Workers (100k/день). Збираючи багато
// дрібних файлів в один запит, ми економимо ліміт без втрати швидкості
// (R2 GET всередині Worker — це внутрішній виклик біндингу, а не мережевий
// запит користувача, тож можна стрімити багато файлів підряд за один HTTP-виклик).
//
// Запит:
//   POST /batch
//   Headers: User-Agent, X-Shaurma-Token — як у звичайних запитах
//   Body (JSON): { "paths": ["packs/foo/mod-storage/a.jar", "packs/foo/..."] }
//
// Відповідь (200 OK, потокова, Content-Type: application/x-shaurma-batch):
//   4 байти   — magic "SHBR" (0x53484252, big-endian) — сигнатура формату
//   Далі для кожного запитаного шляху, в тому ж порядку що в запиті, один запис:
//     1 байт    — статус: 1 = OK (далі йдуть дані), 0 = NOT_FOUND (даних нема)
//     4 байти   — довжина шляху (UTF-8) у байтах, big-endian uint32
//     N байт    — шлях (UTF-8), той самий рядок що прийшов у запиті
//     8 байт    — довжина даних, big-endian uint64 (тільки якщо статус = 1)
//     M байт    — самі дані файлу                  (тільки якщо статус = 1)
//
// Формат навмисно простий (без meta, без JSON-обгортки навколо бінарних
// даних) — щоб Java-клієнт міг читати його стрімом через DataInputStream
// без буферизації всього тіла відповіді в пам'яті, і щоб Worker міг віддавати
// дані по мірі надходження з R2 (не чекаючи завантаження всіх файлів одразу).
// ═══════════════════════════════════════════════════════════════════════════
async function handleBatch(request, env) {
  // ── User-Agent ──────────────────────────────────────────────────────────
  const ua = request.headers.get('User-Agent') || '';
  if (!ua.startsWith('ShaurmaLauncher/')) {
    return errResp(404, 'Not Found');
  }

  // ── Токен ───────────────────────────────────────────────────────────────
  const token = (request.headers.get('X-Shaurma-Token') || '').trim();
  if (token.length < 16) {
    return errResp(401, 'Unauthorized');
  }
  const tokenType = resolveTokenType(token, env);
  if (!tokenType) {
    await randomDelay(50, 150);
    return errResp(403, 'Forbidden');
  }

  // ── Розмір тіла запиту (захист від memory bomb) ───────────────────────
  const cl = parseInt(request.headers.get('content-length') || '0');
  if (cl <= 0 || cl > BATCH_MAX_BODY_BYTES) {
    return errResp(400, 'Bad Request');
  }

  // ── Парсинг JSON-тіла ───────────────────────────────────────────────────
  let payload;
  try {
    payload = await request.json();
  } catch (err) {
    return errResp(400, 'Invalid JSON body');
  }

  const paths = payload && Array.isArray(payload.paths) ? payload.paths : null;
  if (!paths || paths.length === 0) {
    return errResp(400, 'paths must be a non-empty array');
  }
  if (paths.length > BATCH_MAX_FILES) {
    return errResp(400, `Too many paths (max ${BATCH_MAX_FILES})`);
  }

  // ── Дублікати заборонені ─────────────────────────────────────────────
  // Без цього один шлях у списку N разів змусив би Worker N разів читати
  // й стрімити той самий файл з R2 в одній відповіді — корисного навантаження
  // нуль, а R2 read-операцій та egress-трафіку на "1 запит" — як за N запитів.
  const seen = new Set();
  for (const p of paths) {
    if (typeof p !== 'string' || p.length === 0 || p.length > 512) {
      return errResp(400, 'Invalid path entry');
    }
    if (p.includes('..') || p.includes('//') || p.includes('\x00') || p.includes('\\')) {
      return errResp(400, 'Bad Request');
    }
    if (seen.has(p)) {
      return errResp(400, `Duplicate path in batch: ${p}`);
    }
    seen.add(p);
  }

  // ── Резолвимо кожен шлях у R2-ключ з перевіркою прав, ОДРАЗУ (до стріму) ─
  // Якщо хоч один шлях заборонений токеном — відмовляємо всьому запиту,
  // а не мовчки пропускаємо файл. Так клієнт одразу бачить помилку прав,
  // а не плутається чому файл "NOT_FOUND".
  const resolved = [];
  for (const p of paths) {
    const normalized = '/' + p.replace(/^\/+/, '');
    const r2Key = resolveR2Key(normalized, tokenType);
    if (r2Key === null) {
      return errResp(403, `Forbidden: ${p}`);
    }
    resolved.push({ requestPath: p, r2Key });
  }

  // ── Перевірка сумарного розміру ПЕРЕД стрімом ───────────────────────────
  // head() — окремий R2-subrequest, але це швидко (без читання тіла) і дає
  // нам реальний розмір кожного файлу. Якщо сумарний розмір batch занадто
  // великий — відмовляємо одразу 413, замість того щоб почати стрімити
  // гігабайти і впертись у клієнтський таймаут чи зайвий egress.
  // /batch призначений для МНОГО ДРІБНИХ файлів — великі файли (mrpack,
  // java-runtime) мають йти звичним GET/Range-шляхом, не через batch.
  let totalSize = 0;
  for (const { requestPath, r2Key } of resolved) {
    let head;
    try {
      head = await env.R2_BUCKET.head(r2Key);
    } catch (err) {
      console.error('R2 head error in batch precheck:', r2Key, err.message);
      continue; // якщо не вдалось дізнатись розмір — нехай streamBatch сам розбереться (404 чи помилка)
    }
    if (head) {
      // Жорсткий бар'єр на розмір ОДНОГО файлу — окремо від сумарного ліміту.
      // Без цього великий файл (напр. tacz.zip) проходить перевірку сумарного
      // розміру батчу, але обривається посеред стріму через ліміт часу
      // виконання Worker'а. Клієнт має якнайшвидше дізнатись, що цей шлях
      // треба качати через /file, а не мовчки отримувати обірваний потік.
      if (head.size > BATCH_MAX_SINGLE_FILE_BYTES) {
        return errResp(413,
          `File too large for /batch: ${requestPath} (${head.size} bytes, max ${BATCH_MAX_SINGLE_FILE_BYTES} per file) — use /file instead`);
      }
      totalSize += head.size;
    }
  }
  if (totalSize > BATCH_MAX_TOTAL_BYTES) {
    return errResp(413, `Batch too large: ${totalSize} bytes (max ${BATCH_MAX_TOTAL_BYTES})`);
  }

  // ── Стрімінгова відповідь ──────────────────────────────────────────────
  // Кожен файл читаємо з R2 і пишемо в вихідний стрім по мірі готовності,
  // не тримаючи декілька великих файлів в пам'яті одночасно.
  const { readable, writable } = new TransformStream();
  const writer = writable.getWriter();

  // Запускаємо у фоні (не блокуємо повернення Response).
  streamBatch(resolved, env, writer).catch(err => {
    console.error('Batch stream error:', err.message);
    try { writer.abort(err); } catch (_) {}
  });

  return new Response(readable, {
    status: 200,
    headers: {
      'Content-Type':           'application/x-shaurma-batch',
      'Cache-Control':          'no-cache, no-store',
      'X-Content-Type-Options': 'nosniff',
      ...corsHeaders(),
    },
  });
}

async function streamBatch(resolved, env, writer) {
  await writer.write(uint32BE(BATCH_MAGIC));

  for (const { requestPath, r2Key } of resolved) {
    const pathBytes = new TextEncoder().encode(requestPath);

    let object;
    try {
      object = await env.R2_BUCKET.get(r2Key);
    } catch (err) {
      console.error('R2 get error in batch:', r2Key, err.message);
      object = null;
    }

    if (!object) {
      // статус 0 = NOT_FOUND
      await writer.write(new Uint8Array([0]));
      await writer.write(uint32BE(pathBytes.length));
      await writer.write(pathBytes);
      continue;
    }

    // статус 1 = OK
    await writer.write(new Uint8Array([1]));
    await writer.write(uint32BE(pathBytes.length));
    await writer.write(pathBytes);
    await writer.write(uint64BE(object.size));

    // Пишемо тіло файлу стрімом, чанк за чанком — великий файл не лежить
    // увесь в пам'яті одночасно.
    const reader = object.body.getReader();
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        await writer.write(value);
      }
    } finally {
      reader.releaseLock();
    }
  }

  await writer.close();
}

function uint32BE(n) {
  const buf = new Uint8Array(4);
  buf[0] = (n >>> 24) & 0xff;
  buf[1] = (n >>> 16) & 0xff;
  buf[2] = (n >>> 8)  & 0xff;
  buf[3] = n & 0xff;
  return buf;
}

function uint64BE(n) {
  // R2 size — звичайне JS number, безпечно до 2^53. Достатньо для будь-якого
  // реалістичного розміру файлу модпака.
  const buf = new Uint8Array(8);
  const big = BigInt(n);
  for (let i = 7; i >= 0; i--) {
    buf[i] = Number((big >> BigInt((7 - i) * 8)) & 0xffn);
  }
  return buf;
}

// ═══════════════════════════════════════════════════════════════════════════
// resolveTokenType
// ═══════════════════════════════════════════════════════════════════════════
function resolveTokenType(token, env) {
  if (env.TOKEN_SHAURMA && timingSafeEqual(token, env.TOKEN_SHAURMA)) return 'shaurma';
  if (env.TOKEN_BASE    && timingSafeEqual(token, env.TOKEN_BASE))    return 'base';
  return null;
}

// Timing-safe рядкове порівняння (захист від timing attack на токен)
function timingSafeEqual(a, b) {
  if (!a || !b) return false;
  // Порівнюємо по максимальній довжині обох рядків — однаковий час незалежно від збігу
  const len = Math.max(a.length, b.length);
  let diff   = a.length ^ b.length; // якщо довжини різні — diff != 0
  for (let i = 0; i < len; i++) {
    diff |= (a.charCodeAt(i) || 0) ^ (b.charCodeAt(i) || 0);
  }
  return diff === 0;
}

// ═══════════════════════════════════════════════════════════════════════════
// resolveR2Key — маппінг URL-path → R2 ключ
//
// Правила іменування regex:
//   ^[a-zA-Z0-9._\-]+$          — ім'я файлу (без слешів)
//   ^[a-zA-Z0-9._\-/]+$         — шлях у підпапці (без ..)
//   Пробіл у назві exe навмисно дозволений тільки для shaurma (де зараз
//   лежить "Shaurm Launcher-2.1.0.exe" з пробілом — зворотна сумісність).
//
// Повертає null якщо доступ заборонено.
// ═══════════════════════════════════════════════════════════════════════════
function resolveR2Key(path, tokenType) {
  // Прибираємо ведучий /
  const p = path.replace(/^\/+/, '');

  // ── SHAURMA: повний доступ ─────────────────────────────────────────────
  if (tokenType === 'shaurma') {
    // Метадані лаунчера (стара точка сумісності — корінь бакета)
    if (p === 'launcher-version.json')
      return 'launchers/shaurma/launcher-version.json';

    // Новий compare-by-hash апдейтер (shrm-updater): launcher-version.json
    // МІСТИТЬ довільні відносні шляхи компонентів (app/*.jar, app/*.cfg,
    // updater/shrm-updater.exe, app/resources/css/*.css і т.д.) — не лише
    // одиничні .exe в корені. Кожен компонент публікується Pack Manager'ом
    // за ключем launchers/shaurma/<той самий відносний шлях>, тому тут
    // дозволяємо БУДЬ-ЯКИЙ шлях під launcher/**, доки він проходить базову
    // санітизацію (regex нижче — без .. // \0, символи файлової системи).
    if (p === 'launcher/launcher-version.json')
      return 'launchers/shaurma/launcher-version.json';
    if (/^launcher\/[a-zA-Z0-9._\- \/]+$/.test(p) && !/(^|\/)\.\.($|\/)/.test(p))
      return 'launchers/shaurma/' + p.slice('launcher/'.length);

    // Маніфест і індекс збірок
    // packs-index.json — легкий корінний індекс V2 (DOWNLOAD_SYNC_DESIGN_V2.md,
    // розділ 2.1): список збірок + шлях/ETag до manifest.json КОЖНОЇ збірки.
    // server-manifest.json/index.json лишаються для зворотної сумісності зі
    // старими білдами лаунчера — новий лаунчер їх більше не читає.
    if (p === 'packs-index.json')     return 'packs-index.json';
    if (p === 'server-manifest.json') return 'server-manifest.json';
    if (p === 'index.json')           return 'index.json';

    // Модпаки: /packs/<id>/...
    // Перший сегмент (pack ID) — без слешів; далі дозволено будь-які символи крім / та null.
    // Декодування %-encoding вже виконано до виклику resolveR2Key.
    if (/^packs\/[^\/\x00]+(\/[^\/\x00]+)*$/.test(p))
      return p;

    // Java файли збірок: /java/...
    if (/^java\/[^\/\x00]+(\/[^\/\x00]+)*$/.test(p))
      return p;

    // Все інше — заборонено
    return null;
  }

  // ── BASE: тільки базовий лаунчер ──────────────────────────────────────
  if (tokenType === 'base') {
    if (p === 'launcher-version.json')
      return 'launchers/base/launcher-version.json';

    if (p === 'launcher/launcher-version.json')
      return 'launchers/base/launcher-version.json';
    if (/^launcher\/[a-zA-Z0-9._\- \/]+$/.test(p) && !/(^|\/)\.\.($|\/)/.test(p))
      return 'launchers/base/' + p.slice('launcher/'.length);

    // Нічого більше
    return null;
  }

  return null;
}

// ═══════════════════════════════════════════════════════════════════════════
// Хелпери
// ═══════════════════════════════════════════════════════════════════════════

function parseRange(header) {
  if (!header) return undefined;
  const m = header.match(/^bytes=(\d+)-(\d*)$/);
  if (!m) return undefined;
  const offset = parseInt(m[1], 10);
  const end    = m[2] ? parseInt(m[2], 10) : undefined;
  return end !== undefined
    ? { offset, length: end - offset + 1 }
    : { offset };
}

function buildHeaders(object, r2Key) {
  const h = new Headers();

  // Content-Type: визначаємо по розширенню якщо R2 не передав
  const ct = object.httpMetadata?.contentType || guessMime(r2Key);
  h.set('Content-Type', ct);

  // Кешування: JSON — no-cache (часто змінюється), бінарники — 1 день
  h.set('Cache-Control', ct.includes('json') ? 'no-cache, no-store' : 'public, max-age=86400');

  if (object.httpEtag)  h.set('ETag', object.httpEtag);
  if (object.size)      h.set('Content-Length', String(object.size));
  if (object.uploaded)  h.set('Last-Modified', object.uploaded.toUTCString());

  h.set('Accept-Ranges', 'bytes');
  h.set('X-Content-Type-Options', 'nosniff');
  h.set('X-Frame-Options', 'DENY');

  // CORS
  for (const [k, v] of Object.entries(corsHeaders())) h.set(k, v);
  return h;
}

function guessMime(key) {
  if (key.endsWith('.json'))  return 'application/json; charset=utf-8';
  if (key.endsWith('.exe'))   return 'application/octet-stream';
  if (key.endsWith('.mrpack')) return 'application/zip';
  if (key.endsWith('.zip'))   return 'application/zip';
  if (key.endsWith('.jar'))   return 'application/java-archive';
  if (key.endsWith('.png'))   return 'image/png';
  return 'application/octet-stream';
}

function corsHeaders() {
  return {
    'Access-Control-Allow-Origin':  '*',
    'Access-Control-Allow-Methods': 'GET, HEAD, OPTIONS',
    'Access-Control-Allow-Headers': 'X-Shaurma-Token, User-Agent, Range, Content-Type',
  };
}

function errResp(status, message) {
  return new Response(
    JSON.stringify({ error: message }),
    {
      status,
      headers: {
        'Content-Type':           'application/json; charset=utf-8',
        'X-Content-Type-Options': 'nosniff',
        ...corsHeaders(),
      },
    }
  );
}

async function randomDelay(minMs, maxMs) {
  const ms = minMs + Math.floor(Math.random() * (maxMs - minMs));
  await new Promise(r => setTimeout(r, ms));
}

function tryDecodeURIComponent(encoded) {
  try {
    return decodeURIComponent(encoded);
  } catch {
    return encoded;
  }
}