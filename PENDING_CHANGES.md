# PENDING CHANGES

<!--
ЦЕЙ ФАЙЛ ВЕДЕ НЕЙРОНКА. Кожна зміна проєкту = новий пункт тут (правило з AGENTS.md).
Pack Manager після успішної збірки сам зробить git commit + push, візьме звідси
текст для commit-повідомлення і ОЧИСТИТЬ відправлені пункти. Нічого руками не чисти.

Формат: новий блок "## YYYY-MM-DD HH:MM" внизу, під ним пункти:
  - [тип] Опис українською || English commit text (необов'язково)
Типи: feat, fix, perf, refactor, docs, build, ci, chore.
Пункт = одна змістовна зміна, одним рядком, без секретів і без згадок ШІ.
-->

## 2026-11-04 14:00
- [feat] Модуль довіри shaurma-trust: keyset із ролями root/release/server, маніфест оновлень v2, master.cert, токени доступу й активації, доменні префікси підписів || add shaurma-trust module with keyset, v2 update manifest, master cert and signed tokens
- [feat] Нова система оновлень: сигнал від api.shaurma-launcher.org і запасний шлях через GitHub Releases, адреси файлів будує лише клієнт || add new update engine with worker signal and GitHub releases fallback
- [feat] Захист оновлень: підпис release-ключем, заборона відкату (highest_seen), термін дії, канал, білий список хостів редиректу, обмеження rollout сервером || enforce signature, anti-rollback, expiry and redirect allowlist for updates
- [feat] Перевірка Authenticode завантажених exe/dll з видавцями з keyset (WinVerifyTrust) || verify Authenticode of downloaded binaries against keyset signers
- [feat] Двофазне оновлення: стан swapped, підтвердження backend через update-health.json і автоматичний відкат після трьох невдалих стартів || add two-phase update with health check and automatic rollback
- [feat] Lock-файл проти двох одночасних оновлень і перевірка вільного місця || add update lock file and free space check
- [feat] Ворота сертифіката для мосту R1: updater повної редакції розпаковує вшитий master.cert у теку лаунчера до будь-якого оновлення || add certificate gate that unpacks embedded master cert before updating
- [feat] Shell перевіряє відкладені .new-файли за доказом v2 (keyset + маніфест) || verify pending components with v2 proofs in shell
- [feat] Backend позначає версію робочою через 20 с після старту || mark launcher version healthy after startup
- [fix] Production-збірки завжди вимагають підписаний маніфест оновлення || always require signed update manifests in production builds
- [build] scripts/build-windows.ps1 вшиває root-ключ і keyset нової системи зі змінних середовища (без сертифіката) || embed new update system public keys in windows build script

## 2026-11-04 18:00
- [feat] Активація приватного доступу: пошук сертифіката за вмістом, ключ пристрою Ed25519 під DPAPI, перевірка підписаної відповіді сервера, видалення cert лише після збереженого ключа || add access activation with device key and signed server response
- [feat] Короткоживучий токен доступу X-Shaurma-Entitlement замість вшитого токена в єдиному лаунчері; секретні заголовки не йдуть на чужий хост при редиректі || use short-lived entitlement header instead of embedded channel token
- [feat] Налаштування → «Доступ»: вибір файлу, перетягування, вставлення тексту, відв'язування пристрою; вкладка збірок з'являється без перезапуску || add access section to settings
- [feat] «Перевірити оновлення» в єдиному лаунчері питає сигнал нового сервера або GitHub || check updates via new signal server or github in single launcher
- [feat] Інсталятор копіює сертифікат доступу, що лежить поруч із setup.exe, у теку лаунчера || copy access certificate next to setup into install folder
- [ci] Один workflow build-launcher.yml лише за підписаним замовленням: збірка заданого коміту з main, версія з installer/VERSION, без кешів і токенів, attestation на фінальних файлах, чернетка релізу || add single order-signed build-launcher workflow
- [ci] Видалено build-windows.yml, release.yml, release-full.yml і release-local.yml (тег-тригери, секрети в збірці, реліз без підпису) || remove legacy release workflows
- [build] installer/VERSION — версія релізу в коміті || add installer/VERSION
- [build] Скрипти: set-github-secret.ps1 (прихований ввід), audit-github-settings.ps1 (лише читання), sync-github-secrets.ps1 без токена з файлу, build-windows.ps1 вшиває ключі доступу в backend || add github secret and audit scripts, drop file token fallback
- [docs] README і PRIVACY: нова система оновлень, політика підпису, дані активації; SYSTEM-CHANGES.txt оновлено || document new update system, signing policy and activation privacy
- [ci] build-launcher.yml вшиває ключі CurseForge і входу Google зі секретів GitHub, як і раніше (без серверного проксі) || keep embedding curseforge and google sign-in keys in build-launcher workflow

## 2026-11-04 21:00
- [build] Режим -Bundle: setup.exe єдиного лаунчера ставить усі компоненти одразу, без докачки з CDN за токеном; local-only.flag лише для локальної офлайн-збірки || add bundle installer mode for the single launcher
- [ci] build-launcher.yml збирає інсталятор у режимі -Bundle || build bundled installer in build-launcher workflow
- [feat] Shell відновлює відсутні компоненти з GitHub Releases за підписаним маніфестом v2, якщо токена CDN немає || restore missing components from github releases via signed v2 manifest
- [fix] Деінсталятор прибирає файли нової системи оновлень і доступу || remove new update and access files on uninstall
- [build] collect-licenses.ps1 дозволяє лише одну локальну заміну модуля — власний updater-backend/trust || allow own trust module replacement in license collection
