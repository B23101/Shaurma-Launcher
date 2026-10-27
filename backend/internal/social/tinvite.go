package social

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Тимчасові P2P-запрошення (VPS: /friends/tinvite/*). Усі виклики — тим самим
// автентифікованим клієнтом друзів (JWT + HWID), з тим самим бюджетом запитів.

type TempInviteMeta struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Kind    string `json:"kind"`
	Motd    string `json:"motd"`
	Version string `json:"version"`
	Players int    `json:"players"`
	Max     int    `json:"max"`
}

type TempInviteState struct {
	Address string   `json:"address"`
	ID      string   `json:"id"`
	Active  bool     `json:"active"`
	Host    bool     `json:"host"`
	Kind    string   `json:"kind"`
	Motd    string   `json:"motd"`
	Version string   `json:"version"`
	Players int      `json:"players"`
	Max     int      `json:"max"`
	Guests  []string `json:"guests,omitempty"`
	// Permanent/Status — постійна P2P-кімната сервера: online | offline | deleted (без терміну дії).
	Permanent bool   `json:"permanent,omitempty"`
	Status    string `json:"status,omitempty"`
}

type TempTicket struct {
	ID        string `json:"id"`
	Ticket    string `json:"ticket"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (s *Service) TempInviteCreate(m TempInviteMeta) (string, error) {
	var r struct {
		ID string `json:"id"`
	}
	err := s.client.call(http.MethodPost, "/friends/tinvite/create", m, &r)
	return r.ID, err
}

func (s *Service) TempInviteAddGuests(id string, accountIDs []string) error {
	return s.client.call(http.MethodPost, "/friends/tinvite/guests", map[string]any{"id": id, "add": accountIDs}, nil)
}

// TempInviteBeat — false, якщо VPS каже, що запрошення вже мертве (410).
func (s *Service) TempInviteBeat(id string, players int) (alive bool, err error) {
	err = s.client.call(http.MethodPost, "/friends/tinvite/beat", map[string]any{"id": id, "players": players}, nil)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusGone {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) TempInviteClose(id string) error {
	return s.client.call(http.MethodPost, "/friends/tinvite/close", map[string]string{"id": id}, nil)
}

func (s *Service) TempInviteTicket(address string) (TempTicket, error) {
	var t TempTicket
	err := s.client.call(http.MethodPost, "/friends/tinvite/ticket", map[string]string{"address": address}, &t)
	return t, err
}

func (s *Service) TempInviteAdmit(id, ticket string) bool {
	var r struct {
		OK bool `json:"ok"`
	}
	return s.client.call(http.MethodPost, "/friends/tinvite/admit", map[string]string{"id": id, "ticket": ticket}, &r) == nil && r.OK
}

func (s *Service) TempInviteStates(addresses []string) ([]TempInviteState, error) {
	var r struct {
		Items []TempInviteState `json:"items"`
	}
	if len(addresses) == 0 {
		return nil, nil
	}
	err := s.client.call(http.MethodGet, "/friends/tinvite/state?addr="+url.QueryEscape(strings.Join(addresses, ",")), nil, &r)
	return r.Items, err
}

func (s *Service) TempInviteMine() ([]TempInviteState, error) {
	var r struct {
		Items []TempInviteState `json:"items"`
	}
	err := s.client.call(http.MethodGet, "/friends/tinvite/mine", nil, &r)
	return r.Items, err
}
