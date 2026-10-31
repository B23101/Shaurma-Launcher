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

## 2026-10-31 23:58
- [fix] Очищення відновлюваних кешів WebView2 при повторному запуску зі збереженням даних профілю; додано регресійні тести || clean disposable webview caches on startup while preserving profile data
- [fix] Очищення залишків оновлення більше не залежить від доступності бази даних || run installation garbage cleanup independently of database availability

## 2026-11-01 01:02
- [feat] Діагностика P2P та налаштування власних правил брандмауера через явний системний дозвіл без запуску скриптів || add p2p diagnostics and consent-based native firewall repair
- [fix] Повторний запуск P2P використовує нову теку стану та припиняється після скасування; додано регресійні тести || use fresh state for p2p recovery and respect cancellation
- [feat] Спільне вікно допомоги після мережевих збоїв гри та передавання збірок із повторною перевіркою в налаштуваннях || surface p2p recovery guidance after network failures and in settings
- [fix] Повторний запуск гостьового P2P-слухача після усунення помилки порту без переривання активних сесій || retry failed p2p listeners without interrupting active sessions
- [docs] Уточнено перевірку мережі й добровільне налаштування дозволів Windows у політиці приватності || document p2p diagnostics and optional windows permission repair

## 2026-11-01 01:15
- [fix] Прибрано автоматичне читання буфера обміну для запрошень у друзі при запуску, фокусуванні та у фоні; збережено явне відкриття посилань || remove automatic clipboard polling for friend invitations while preserving explicit links
