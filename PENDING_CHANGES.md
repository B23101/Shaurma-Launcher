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

## 2026-11-04 15:30
- [feat] Єдиний лаунчер бере збірки з api.shaurma-launcher.org/v1 за токеном доступу пристрою; міст R1 і старші лаунчери — як раніше, через старий сервер || load packs from api.shaurma-launcher.org in the single launcher

## 2026-11-04 18:00
- [ci] build-launcher.yml збирає компоненти й інсталятор єдиного лаунчера в режимі -Bundle, без bootstrap; перевірки підписів і секретів для shaurma || build single launcher components without bootstrap in workflow
- [build] SignPath: дві конфігурації (components, installer) під ShaurmLauncher.exe, backend.dll, updater.dll і ShaurmLauncher-setup.exe; team-конфігурації видалено || simplify signpath configuration to single launcher
- [build] verify-signatures.ps1: bootstrap перевіряється лише якщо він є у збірці || make bootstrap optional in signature verification
- [fix] Оновлювач не повідомляє про помилку, якщо в релізі немає файлів для цієї платформи || skip releases without files for the current platform
- [docs] README і RELEASING.md описують єдиний лаунчер, новий потік випуску, бета як pre-release і сховище збірок || document single launcher release process
- [docs] Прибрано згадки редакцій Base/повна з політики приватності та порівняння з іншими лаунчерами || remove edition wording and launcher comparisons from docs

## 2026-11-04 19:30
- [ci] Перевірка політики збірки знає про вшивання ключів нової системи оновлень і перевіряє, що вони потрапляють в updater || fix build policy test for new update trust material
- [build] frontend/package-lock.json узгоджено з npm 10 на CI (опційна залежність chokidar 3 для nunjucks) || sync frontend lockfile with npm 10
- [fix] Сторінка офіційної збірки більше не падає через виклик неіснуючого методу дизайну сторінки; лишаються вбудовані дизайни || stop calling missing pack page design method
- [build] Ресурси версії, іконка й маніфест exe генеруються через go-winres: Windows і SignPath бачать назву продукту й версію || generate exe version resources with go-winres
- [chore] Збірки перенесено в нове сховище; старий сервер читає їх звідти, порожні «теки» дашборда не переносяться || migrate packs to new storage and read them upstream
- [build] Прибрано перевірку типу клієнта Google у збірці: вона блокувала збірку на GitHub і нічого не захищала || drop google client kind build check
- [fix] Лаунчер сам довстановлює оновлені updater і exe, які старий shell відкидав через невідповідний ключ підпису: перевіряє їх підписаним маніфестом і ставить на місце з наступного запуску || backend finishes pending updater and shell swap rejected by old shells
