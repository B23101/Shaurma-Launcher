package main

// Коментарі в профілі (Wails-методи). Логіка, санітизація й клієнтські перевірки — в
// internal/social/comments.go; ліміти (40 символів, 3 від автора, 30 на профіль) тримає сервер.

import "shaurma-launcher-backend/internal/social"

// GetProfileComments — коментарі профілю друга (або мого власного).
func (a *App) GetProfileComments(accountID string) (social.ProfileComments, error) {
	return a.socialService().ProfileComments(accountID)
}

// PostProfileComment — залишити коментар у профілі друга.
func (a *App) PostProfileComment(accountID, text string) error {
	return a.socialService().PostProfileComment(accountID, text)
}

// DeleteProfileComment — видалити коментар (свій або під моїм профілем).
func (a *App) DeleteProfileComment(commentID int64) error {
	return a.socialService().DeleteProfileComment(commentID)
}

// ReactProfileComment — поставити реакцію під коментарем; порожній emoji знімає її.
func (a *App) ReactProfileComment(commentID int64, emoji string) error {
	return a.socialService().ReactProfileComment(commentID, emoji)
}
