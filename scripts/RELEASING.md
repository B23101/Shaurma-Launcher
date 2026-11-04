# Випуск лаунчера

Один лаунчер, один workflow (`.github/workflows/build-launcher.yml`), один ланцюг довіри.
GitHub — недовірений будівельник: ключів підпису в нього немає. Офіційним реліз робить лише
маніфест, який підписує Pack Manager після перевірки attestation.

## Ролі ключів

| Ключ | Де | Що підписує |
|---|---|---|
| `root` | офлайн-носій, з резервною копією | лише `keyset.json` і `master.cert` |
| `release` | `pack-manager\config\trust-private` | маніфест оновлення, замовлення збірки, сигнал версії, звіт |
| `server` | секрет `SERVER_SIGNING_KEY` нового worker | відповіді активації і токени доступу |

Ключі створює `pack-manager/cmd/trustctl` (`root-init`, `release-init`, `server-init`, `keyset`, `cert`,
`verify`). Публічна частина (`root.pub`, `keyset.json`, `.sig`) лежить у `pack-manager\config\trust` і
вшивається у збірки. Ротація `release`/`server`: новий `keyset` з більшою версією, підписаний `root`, —
клієнти приймають його з наступного релізу без перевстановлення.

## Одноразове налаштування

1. GitHub (лише від вашого входу `gh`): environment `release`; змінні `ORDER_PUBKEYS` (release.pub),
   `SHAURMA_ROOT_PUBKEYS`, `SHAURMA_KEYSET_B64`, `SHAURMA_KEYSET_SIG`, `SHAURMA_MANIFEST_PUBKEYS`;
   секрети CurseForge і входу Google — як і раніше. Перевірка: `scripts/audit-github-settings.ps1`
   (лише читання). Секрети ставить `scripts/set-github-secret.ps1` (прихований ввід).
2. SignPath: див. `.signpath/README.md` (конфігурації `components` і `installer`, політики
   `release-signing` з ручним погодженням і `test-signing`, MFA).
3. Новий worker `api.shaurma-launcher.org`: секрети `SERVER_SIGNING_KEY`, `ADMIN_KEY`, `INTERNAL_KEY`,
   `SESSION_SIGNING_KEY`.

## Випуск (Pack Manager → «Новий потік (R2)»)

1. Введіть версію: `X.Y.Z` — стабільна (на GitHub *Latest*), `X.Y.Z.W` (W ≠ 0) — бета (*pre-release*,
   лише для бета-каналу).
2. Pack Manager сам: виставляє `installer/VERSION`, комітить код із текстом з `PENDING_CHANGES.md` і
   пушить його вашим `git` (без force, без секретів і бінарників); підписує замовлення; запускає
   workflow; чекає (погодження SignPath — це очікування, а не збій); перевіряє SHA-256 і attestation
   (той самий коміт, workflow, гілка `main`); підписує `launcher-manifest.json`; публікує реліз із
   автоматичним текстом (Нове / Виправлення / Швидкодія + перевірка справжності); надсилає сигнал
   версії на `api.shaurma-launcher.org`; пише підписаний звіт у `releases-frozen\r2-reports`.
3. Без SignPath реліз виходить з `authenticode: waived` у підписаному маніфесті: безпека оновлень від
   Authenticode не залежить.

## Міст для старих лаунчерів (старий потік)

Старий worker (`shaurma-proxy`) і його сховище лишаються лише для оновлень лаунчера (`launchers/...`):
так завжди можна довести старий лаунчер до нової системи. Міст збирається локально («Збірка лаунчера»
з галочкою «Заморозити міст R1»), заморожується в `releases-frozen\bridge-r1\<версія>` і заливається
вкладкою «Оновлення лаунчера» в «Старе сховище оновлень лаунчера». Нові версії на старий worker не
заливаються — Pack Manager це блокує.

## Збірки (модпаки)

Збірки, `acl.json` і `java/` живуть лише в новому сховищі (R2 `shaurma-packs`, акаунт shaurma-site).
Pack Manager пише їх напряму в R2 (вкладка R2). Новий лаунчер бере їх з `api.shaurma-launcher.org/v1`
за токеном активованого пристрою; старі лаунчери — через старий worker, який читає те саме сховище.

## Аварійні дії

- Погана версія: `revoked_versions` у новому keyset або нова версія з `min_version`/`security`.
- Витік `release`/`server`: новий ключ → новий keyset (підпис `root`) → реліз.
- Відкликати пристрій: адмінка нового worker `/v1/admin/devices/<id>/revoke`.
- Сертифікат потрапив не туди: новий `master.cert` з більшою епохою і `MIN_CERT_EPOCH` на worker.
