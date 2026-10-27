//go:build !windows && !linux

package discordrpc

import "errors"

// ipcConn — заглушка для платформ, відмінних від Windows і Linux (macOS
// виключено з плану підтримки). Linux має власну реалізацію в
// ipc_linux.go (Unix-сокет $XDG_RUNTIME_DIR/discord-ipc-N).
type ipcConn struct{}

func ipcDial() (*ipcConn, error) { return nil, errors.New("discord rpc: підтримується лише на Windows") }

func (c *ipcConn) write(uint32, []byte) error { return errors.New("discord rpc: не підтримується") }
func (c *ipcConn) read([]byte) (int, error)   { return 0, errors.New("discord rpc: не підтримується") }
func (c *ipcConn) close()                     {}
