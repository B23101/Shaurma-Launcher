//go:build !windows

package main

import "errors"

func elevateP2PFirewall() error { return errors.New("p2p_repair_unavailable") }
