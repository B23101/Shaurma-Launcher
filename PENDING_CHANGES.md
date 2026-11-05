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

## 2026-11-05 13:14
- [fix] Додано групи до спільного вибору одержувачів контенту, збірок і серверів без окремої кнопки || unify group and friend sharing recipient selection
- [fix] Додано запрошення P2P з групового чату з попереднім вибором усіх учасників || enable temporary P2P invitations from group chats
- [fix] Замінено обертання іконки завантаження на вертикальний рух на сторінці збірки || animate pack download icons vertically

## 2026-11-05 13:23
- [fix] Об’єднано групові й особисті чати та впорядковано за останнім повідомленням || merge group and private chats by latest message time
- [feat] Винесено учасників групи в окреме вікно з доступними статусами та керуванням складом || separate group members and show available presence
- [fix] Оновлено налаштування групи та прибрано кольорову рамку чату || align group settings with launcher controls and remove colored chat borders
- [fix] Освітлено акценти з адаптацією контрасту світлої теми та оновлено спільні кнопки й анімацію натискання || brighten accents and refresh shared button styling and press motion

## 2026-11-05 13:35
- [feat] Додано перегляд читачів повідомлення та приглушення групових чатів || show group message readers and add group mute controls
- [fix] Додано назву й аватарку групи до сповіщень та відкриття потрібного чату || identify group notifications and open the matching group
- [feat] Додано групові P2P-запрошення з серверною перевіркою членства; локальна папка vps: міграція 0018 || authorize group P2P invitations with membership checks
- [feat] Додано відкличні посилання на вступ до групи; локальна папка vps: токени зі строком дії та міграція 0018 || add expiring revocable group invite links

## 2026-11-05 13:44
- [chore] Додано серверні перевірки для групового P2P, відкликання посилань і втрати членства; тести у локальній папці vps || test group access authorization and invite revocation

## 2026-11-05 14:20
- [feat] Перероблено сповіщення в треї: власний колір, іконка, мітки й кнопки для кожної події (завантаження, помилка, потрібна дія, запит у друзі, збірка/мод/сервер від друга, тимчасове й постійне P2P, запуск і зупинка сервера друга) || redesign tray notifications with per-event styling and actions
- [fix] Сповіщення тепер складаються стосом (макс. 3) і не замінюють одне одного; прибрано загальний фон вікна || stack tray notifications instead of replacing and drop window backdrop
- [perf] Анімації сповіщень лише transform/opacity, таймер-смужка на CSS без постійних циклів || keep notification animations cheap

## 2026-11-05 14:50
- [fix] Прибрано сіру рамку та блимання навколо вікна сповіщень: вікно більше не має системної рамки/тіні || remove system frame and flicker around notification window

## 2026-11-05 15:10
- [fix] Акценти стали темнішими: середній тон між попередніми темними та світлими, градієнтні акценти теж приглушено || tone accents down to a mid brightness between dark and light
