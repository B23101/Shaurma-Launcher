//go:build shaurma

// Тимчасові сервери у списку ЗБІРКИ: гравець приєднався до P2P-кімнати
// (співхост або тимчасове запрошення) і додав її поруч зі своїми серверами.
// Доступ живе, поки живий світ, тож і запис у servers.dat має жити рівно
// стільки ж: лаунчер сам прибирає його, коли доступу немає — саме це просив
// користувач («лаунчер буде розуміти що це тимчасове; якщо немає доступа —
// видаляє зі списку серверів»).
package main

import (
	"log"
	"strings"
)

// SyncTempPackServers прибирає зі списків збірок тимчасові P2P-сервери, які
// вже недоступні, і повертає адреси, що лишились живими (для UI).
//
// Обережність: якщо ЖОДНА перевірка не відповіла (немає мережі, не
// залогінений), НІЧОГО не прибираємо — інакше один офлайн-старт лаунчера
// зносив би робочі записи.
func (a *App) SyncTempPackServers() []string {
	packs := a.cfg.AllInstanceConfigs()
	addrs := make([]string, 0, 8)
	owner := make(map[string][]string) // адреса → збірки, де вона тимчасова
	for packID, icfg := range packs {
		for _, ip := range icfg.TempServerIPs {
			if _, seen := owner[ip]; !seen {
				addrs = append(addrs, ip)
			}
			owner[ip] = append(owner[ip], packID)
		}
	}
	if len(addrs) == 0 {
		return nil
	}

	alive := make(map[string]bool, len(addrs))
	checked := false
	if svc := a.socialService(); svc != nil {
		if states, err := svc.TempInviteStates(addrs); err == nil {
			checked = true
			for _, s := range states {
				if s.Active {
					alive[strings.ToLower(s.Address)] = true
				}
			}
		}
	}
	if a.p2pAPI != nil {
		if rooms, err := a.p2pAPI.SharedRooms(); err == nil {
			checked = true
			for _, r := range rooms {
				alive[strings.ToLower(r.Address)] = true
				alive[strings.ToLower(r.Subdomain)] = true
			}
		}
	}
	if !checked {
		return addrs
	}

	live := make([]string, 0, len(addrs))
	for _, ip := range addrs {
		if alive[strings.ToLower(ip)] {
			live = append(live, ip)
			continue
		}
		for _, packID := range owner[ip] {
			if err := a.RemovePackServer(packID, ip); err != nil {
				// Файлу може вже не бути — запис усе одно знімаємо.
				log.Printf("temp servers: прибирання %s зі збірки %s: %v", ip, packID, err)
			}
			a.setTempPackServer(packID, ip, false)
		}
	}
	return live
}
