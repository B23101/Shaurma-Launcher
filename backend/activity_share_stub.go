//go:build !shaurma

package main

// activity_share_stub.go — без збірки Shaurma передач друзям немає.

func (a *App) observeShare(name string, payload any) {}
