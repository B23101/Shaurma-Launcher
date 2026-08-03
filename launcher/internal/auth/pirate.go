package auth

import (
	"fmt"
	"net/http"
	"time"

	"shaurma-launcher-wails/internal/model"
)

type PirateService struct {
	client *http.Client
}

func NewPirateService() *PirateService {
	return &PirateService{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *PirateService) Login(username string) (*model.Account, error) {
	if username == "" {
		return nil, fmt.Errorf("username is required")
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

func generateOfflineUUID(username string) string {
	h := 0
	for _, c := range username {
		h = h*31 + int(c)
	}
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", h%1000000000000)
}
