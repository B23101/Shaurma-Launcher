//go:build !windows

package main

func openFileDefault(path string) error { return xdgOpen(path) }
