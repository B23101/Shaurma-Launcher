package auth

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"shaurma-launcher-backend/internal/model"
)

type PirateService struct {
	client *http.Client
}

func NewPirateService() *PirateService {
	return &PirateService{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Minecraft-правила нікнейму (офлайн-режим, ті самі, що приймає гра):
// 3–16 символів, лише латиниця a-z, цифри 0-9 і підкреслення. Кирилиця,
// пробіли, довгі ніки — недопустимі: гра однаково не прийме такий нік
// у офлайн-режимі, тож лаунчер не має створювати акаунт, яким не можна
// зайти в гру.
func validateMinecraftNickname(username string) error {
	if n := utf8.RuneCountInString(username); n < 3 || n > 16 {
		return fmt.Errorf("нікнейм має бути від 3 до 16 символів")
	}
	for _, r := range username {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return fmt.Errorf("нікнейм може містити лише латинські літери, цифри та підкреслення (без кирилиці та пробілів)")
	}
	return nil
}

func (p *PirateService) Login(username string) (*model.Account, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if err := validateMinecraftNickname(username); err != nil {
		return nil, err
	}

	return &model.Account{
		ID:          fmt.Sprintf("offline-%s", username),
		Username:    username,
		Type:        "offline",
		AccessToken: "",
		UUID:        generateOfflineUUID(username),
		IsLicensed:  false,
	}, nil
}

// generateOfflineUUID рахує UUID v3 з нікнейму — ТОЧНО так само, як
// ванільний клієнт Minecraft у офлайн-режимі (Java UUID#nameUUIDFromBytes
// над рядком "OfflinePlayer:<нік>", MD5-дайджест, версія/варіант виставлені
// вручну по RFC 4122). Ідентична реалізація в Prism Launcher
// (MinecraftAccount::uuidFromUsername).
//
// Важливо рахувати ТОЧНО так само, як гра: якщо лаунчер видасть інший
// UUID, ніж порахує сам клієнт при вході в офлайн-режимі, це ламає
// узгодженість (скіни, LAN-ігри з тим самим гравцем після
// перезапуску тощо) — гравець "роздвоюється" на два різні UUID для
// одного й того ж ніка.
func generateOfflineUUID(username string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + username))

	// Виставляємо версію (3) і варіант (RFC 4122/IETF) біти, як велить
	// стандарт UUID v3 — саме ці два байти відрізняють "просто MD5" від
	// валідного UUID.
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
