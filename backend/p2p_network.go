package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/p2p"
)

type P2PNetworkReport struct {
	Supported        bool   `json:"supported"`
	Configured       bool   `json:"configured"`
	Blocked          bool   `json:"blocked"`
	PolicyRestricted bool   `json:"policyRestricted"`
	Control          string `json:"control"`
}

var p2pRepairMu sync.Mutex

func readP2PFirewall() P2PNetworkReport {
	var report P2PNetworkReport
	if runtime.GOOS != "windows" {
		return report
	}
	exe, err := os.Executable()
	if err != nil {
		return report
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, exe, "--p2p-firewall-status").Output()
	if err == nil {
		if err := json.Unmarshal(data, &report); err != nil {
			return P2PNetworkReport{}
		}
	}
	return report
}

// Only diagnoses after a failed operation or an explicit user request. Missing
// allow rules alone do not mean P2P is blocked; default outbound access may work.
func (a *App) DiagnoseP2PNetwork() P2PNetworkReport {
	report := readP2PFirewall()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p2p.ControlURL+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	report.Control = "unreachable"
	if err != nil {
		var dns *net.DNSError
		if errors.As(err, &dns) {
			report.Control = "dns"
		}
	} else {
		resp.Body.Close()
		report.Control = "server"
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			report.Control = "ok"
		}
	}
	return report
}

// Explicit UI action only. Never remove block rules or restart active transfers.
func (a *App) RepairP2PNetwork() (P2PNetworkReport, error) {
	if !p2pRepairMu.TryLock() {
		return P2PNetworkReport{}, errors.New("p2p_repair_busy")
	}
	defer p2pRepairMu.Unlock()
	r := readP2PFirewall()
	if !r.Supported {
		return r, errors.New("p2p_repair_unavailable")
	}
	if r.Blocked || r.PolicyRestricted {
		return r, errors.New("p2p_repair_policy")
	}
	if !r.Configured {
		if err := elevateP2PFirewall(); err != nil {
			return r, err
		}
	}
	r = a.DiagnoseP2PNetwork()
	if !r.Supported || !r.Configured {
		return r, errors.New("p2p_repair_failed")
	}
	if err := a.EnsureP2PGuestService(); err != nil {
		return r, errors.New("p2p_repair_listener")
	}
	return r, nil
}
