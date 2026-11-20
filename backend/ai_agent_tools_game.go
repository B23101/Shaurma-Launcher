package main

// ai_agent_tools_game.go — інструменти керування грою (game_*), оновлення агента та «джерела модів».

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func aiGameCmd(g argMap, cmd string, extra map[string]any) map[string]any {
	req := map[string]any{"cmd": cmd}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

// pass копіює вказані ключі з аргументів інструмента в запит агента.
func pass(g argMap, keys ...string) map[string]any {
	m := map[string]any{}
	for _, k := range keys {
		if v, ok := g[k]; ok {
			m[k] = v
		}
	}
	return m
}

func init() {
	regTool(aiTool{
		name:  "game_status",
		desc:  "Стан запущеної гри всередині: FPS (поточний, середній за 5 с, 1% low, час кадру), розмір вікна, чи захоплений курсор, JVM (heap, GC, потоки, CPU), список модів лоадера, заблоковані мережеві з'єднання. Потрібна гра, запущена ШІ (launch_build) з правом «Керування грою».",
		props: map[string]string{"pack_id": "id збірки (порожньо — єдина активна сесія)", "client": "(int) номер клієнта ШІ 1..4 (порожньо — єдиний активний)"},
		write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "status", nil))
		},
	})
	regTool(aiTool{
		name:  "game_screenshot",
		desc:  "Зробити окремий скріншот гри (не потрапляє в папку скріншотів, вікно може бути позаду). Повертає зображення.",
		props: map[string]string{"pack_id": "id збірки (порожньо — єдина активна)", "client": "(int) номер клієнта ШІ", "max_width": "(int) макс. ширина, 0 — оригінал; за замовч. 960", "format": "png | jpeg (jpeg менший)"},
		write: true,
		run: func(a *App, g argMap) (any, error) {
			out, err := a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "screenshot", pass(g, "max_width", "format")))
			if err != nil {
				return nil, err
			}
			b64, _ := out["data_base64"].(string)
			data, derr := base64.StdEncoding.DecodeString(b64)
			if derr != nil || len(data) == 0 {
				return nil, errors.New("агент повернув некоректне зображення")
			}
			mime, _ := out["mime"].(string)
			if mime == "" {
				mime = "image/png"
			}
			w, _ := out["width"].(float64)
			h, _ := out["height"].(float64)
			return aiImage{Data: data, Mime: mime, Text: "скріншот гри " + itoa(int(w)) + "×" + itoa(int(h))}, nil
		},
	})
	regTool(aiTool{
		name:     "game_key",
		desc:     "Натиснути клавішу в грі. key: w a s d space escape enter tab f1..f12 left_shift ctrl alt e q t slash up/down/left/right тощо. action: tap (натиснути й відпустити), press (утримувати), release. hold_ms — скільки тримати при tap (за замовч. 70). mods: [\"shift\",\"ctrl\",\"alt\"].",
		props:    map[string]string{"key": "ім'я клавіші", "action": "tap | press | release", "hold_ms": "(int) мс", "mods": "масив модифікаторів", "pack_id": "id збірки"},
		required: []string{"key"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "key", pass(g, "key", "action", "hold_ms", "mods")))
		},
	})
	regTool(aiTool{
		name:  "game_mouse_move",
		desc:  "Перемістити курсор: dx/dy — відносний зсув у пікселях (повертає камеру, коли курсор захоплений грою; великі повороти виконуються малими кроками) або x/y — абсолютна позиція у вікні (для меню й інвентарю).",
		props: map[string]string{"dx": "(int) зсув по X", "dy": "(int) зсув по Y", "x": "(int) абсолютна X", "y": "(int) абсолютна Y", "pack_id": "id збірки", "client": "(int) номер клієнта ШІ"},
		write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "mouse_move", pass(g, "dx", "dy", "x", "y")))
		},
	})
	regTool(aiTool{
		name:  "game_mouse_click",
		desc:  "Натиснути кнопку миші: button left | right | middle; action tap | press | release; hold_ms; за потреби x,y — спершу навести курсор.",
		props: map[string]string{"button": "left | right | middle", "action": "tap | press | release", "hold_ms": "(int) мс", "x": "(int) X", "y": "(int) Y", "pack_id": "id збірки", "client": "(int) номер клієнта ШІ"},
		write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "mouse_click", pass(g, "button", "action", "hold_ms", "x", "y")))
		},
	})
	regTool(aiTool{
		name: "game_scroll", desc: "Прокрутити колесо миші (dy>0 вгору; у грі це перемикання слоту хотбара).",
		props: map[string]string{"dy": "(int) кроки", "dx": "(int) кроки по горизонталі", "pack_id": "id збірки", "client": "(int) номер клієнта ШІ"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "scroll", pass(g, "dx", "dy")))
		},
	})
	regTool(aiTool{
		name: "game_type", desc: "Ввести текст у відкрите поле (чат, табличка, ковадло, пошук): символи подаються як при друці. Чат/поле має бути вже відкрите.",
		props: map[string]string{"text": "текст до 500 символів", "delay_ms": "(int) пауза між символами", "pack_id": "id збірки", "client": "(int) номер клієнта ШІ"}, required: []string{"text"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "type", pass(g, "text", "delay_ms")))
		},
	})
	regTool(aiTool{
		name: "game_chat", desc: "Написати в чат гри: відкриває чат (T або «/» для команд), вводить текст і натискає Enter. Команди — з ведучим «/», напр. «/time set day», «/gamemode creative». Працює в одиночному світі й на локальному сервері.",
		props: map[string]string{"text": "повідомлення або команда з «/»", "open_key": "клавіша відкриття чату, якщо змінена (за замовч. t)", "pack_id": "id збірки", "client": "(int) номер клієнта ШІ"}, required: []string{"text"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.gameCall(g.str("pack_id"), g.integer("client", 0), aiGameCmd(g, "chat", pass(g, "text", "open_key")))
		},
	})

	regTool(aiTool{
		name: "list_ai_clients",
		desc: "Запущені клієнти гри ШІ (акаунти Claude, Claude_2…): збірка, нік, номер клієнта, чи є керування. Другий клієнт потрібен для тестів мультиплеєра (інший гравець): launch_build з client=2 і вхід обох на локальний сервер.",
		run: func(a *App, g argMap) (any, error) {
			out := []map[string]any{}
			base := a.aiAgentNickNow()
			max := a.aiMaxClients()
			for i := 1; i <= max; i++ {
				nick := aiClientNick(base, i)
				id, ok := a.aiAccountIDFor(nick)
				row := map[string]any{"client": i, "nick": nick, "account_exists": ok, "running_builds": []string{}}
				if ok {
					var run []string
					for _, s := range a.sessionsM.Snapshot() {
						if s.AccountID == id {
							run = append(run, s.BuildID)
						}
					}
					row["running_builds"] = run
				}
				out = append(out, row)
			}
			aiGames.mu.Lock()
			a.aiPruneGames()
			var ctl []map[string]any
			for _, s := range aiGames.m {
				ctl = append(ctl, map[string]any{"pack_id": s.PackID, "client": s.Client, "nick": s.Nick, "control": s.WithAgent})
			}
			aiGames.mu.Unlock()
			return map[string]any{"max_clients": max, "clients": out, "control_sessions": ctl}, nil
		},
	})
	regTool(aiTool{
		name:  "set_agent_nick",
		desc:  "Обрати власний нік гравця ШІ (3–16 символів: латиниця, цифри, _). За замовчуванням нік береться з назви MCP-клієнта (Claude → Claude, ChatGPT → ChatGPT), тож зазвичай викликати не треба. Нік належить агенту, не користувачу. Не під час гри ШІ; не нік ліцензійного акаунта.",
		props: map[string]string{"nick": "бажаний нік"}, required: []string{"nick"},
		run: func(a *App, g argMap) (any, error) {
			if err := a.aiAgentSetNick(g.str("nick")); err != nil {
				return nil, err
			}
			return map[string]any{"nick": a.aiAgentNickNow()}, nil
		},
	})
	regTool(aiTool{
		name: "get_game_agent_status",
		desc: "Стан компонента «Shaurma Game Agent» (jar, який лаунчер підкладає у запуски ШІ): версія, SHA-256, де лежить, джерело оновлення.",
		run:  func(a *App, g argMap) (any, error) { return a.GetAIGameAgentStatus(), nil },
	})
	regTool(aiTool{
		name:  "update_game_agent",
		desc:  "Встановити або оновити Shaurma Game Agent: лаунчер безпечно качає jar із фіксованого релізу GitHub цього проєкту, перевіряє https, розмір, SHA-256, структуру й маніфест, зберігає попередню версію. Окремо від оновлення лаунчера.",
		write: true,
		run: func(a *App, g argMap) (any, error) {
			return a.UpdateAIGameAgent()
		},
	})

	// ── Джерела модів: прив'язати jar/теку (напр. збірку з IDE) до збірки; лаунчер сам оновлює мод перед запуском ──
	regTool(aiTool{
		name: "list_mod_sources", desc: "Джерела модів, прикріплені до збірки: файл або тека, звідки лаунчер копіює мод у mods/ ПЕРЕД кожним запуском.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			return a.GetModSources(g.str("pack_id"))
		},
	})
	regTool(aiTool{
		name:     "add_mod_source",
		desc:     "Прикріпити джерело мода до збірки: шлях до зібраного .jar (kind=file, напр. build/libs/mymod.jar з IDEA/Gradle) або теки з модами (kind=folder). Не треба копіювати мод вручну: перед кожним запуском збірки лаунчер оновить його зі джерела. Шлях має бути локальним і абсолютним.",
		props:    map[string]string{"pack_id": "id збірки", "path": "абсолютний локальний шлях", "kind": "file | folder (за замовч. file)"},
		required: []string{"pack_id", "path"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "path"); err != nil {
				return nil, err
			}
			kind := g.str("kind")
			if kind == "" {
				kind = "file"
			}
			p, err := aiCheckSourcePath(g.str("path"), kind)
			if err != nil {
				return nil, err
			}
			return a.AddModSource(g.str("pack_id"), kind, p)
		},
	})
	regTool(aiTool{
		name: "detach_mod_source", desc: "Відкріпити джерело мода від збірки (source_id з list_mod_sources). Сам мод у збірці лаунчер повертає до стану до джерела за своїми правилами.",
		props: map[string]string{"pack_id": "id збірки", "source_id": "id джерела"}, required: []string{"pack_id", "source_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "source_id"); err != nil {
				return nil, err
			}
			return a.RemoveModSource(g.str("pack_id"), g.str("source_id"))
		},
	})
	regTool(aiTool{
		name: "sync_mod_sources", desc: "Синхронізувати mods/ збірки з її джерелами зараз (інакше це відбудеться перед запуском). Корисно після нової збірки мода в IDE.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			return a.SyncModSources(g.str("pack_id"))
		},
	})
}

// aiCheckSourcePath перевіряє шлях джерела: абсолютний, локальний (не мережевий UNC), існує, правильного типу.
func aiCheckSourcePath(p, kind string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || len(p) > 1024 || strings.ContainsRune(p, 0) {
		return "", errors.New("некоректний шлях")
	}
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
		return "", errors.New("мережеві шляхи (UNC) не дозволені: вкажіть локальний шлях")
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("потрібен абсолютний шлях")
	}
	abs := filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", errors.New("шлях не знайдено: " + abs)
	}
	switch kind {
	case "file":
		if st.IsDir() || !strings.EqualFold(filepath.Ext(abs), ".jar") {
			return "", errors.New("kind=file: потрібен файл .jar")
		}
		if st.Size() > 256<<20 {
			return "", errors.New("jar завеликий (понад 256 МБ)")
		}
	case "folder":
		if !st.IsDir() {
			return "", errors.New("kind=folder: потрібна тека")
		}
	default:
		return "", errors.New("kind: file | folder")
	}
	return abs, nil
}
