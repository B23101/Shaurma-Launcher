//go:build !shaurma

package main

// Базова (не-Shaurma) редакція: «Поділитися» відсутнє, хуки — порожні.

func (a *App) shareLockErr(kind, refID string) error { return nil }
func (a *App) ShareBusy() bool                        { return false }
func (a *App) shareShutdown()                         {}
