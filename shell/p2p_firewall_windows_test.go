//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// Read-only integration check. Never request elevation or change host rules.
func TestP2PFirewallReadOnly(t *testing.T) {
	err := withFirewall(func(policy, rules *ole.IDispatch, exe string) error {
		if !filepath.IsAbs(exe) {
			t.Fatal("helper target must be its own absolute executable path")
		}
		v, err := oleutil.GetProperty(policy, "CurrentProfileTypes")
		if err != nil {
			return err
		}
		defer v.Clear()
		if firewallInt(policy, "CurrentProfileTypes") < 0 {
			t.Fatal("could not inspect current profiles")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestP2PFirewallStatusProcess(t *testing.T) {
	if os.Getenv("SHAURMA_TEST_FIREWALL_STATUS") == "1" {
		os.Args = []string{os.Args[0], "--p2p-firewall-status"}
		handleP2PFirewallCommand()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestP2PFirewallStatusProcess$")
	cmd.Env = append(os.Environ(), "SHAURMA_TEST_FIREWALL_STATUS=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status helper: %v: %s", err, output)
	}
	var status struct {
		Supported bool `json:"supported"`
	}
	if err := json.Unmarshal(output, &status); err != nil || !status.Supported {
		t.Fatalf("invalid helper response: %s (%v)", output, err)
	}
}

func TestP2PFirewallInstallerRuleNames(t *testing.T) {
	for _, exe := range []string{"ShaurmLauncher.exe", "ShaurmLauncherBase.exe"} {
		got := p2pRuleName(filepath.Join("C:\\Program Files", exe), 1)
		if got != "Shaurm Launcher P2P-"+exe+"-1" {
			t.Fatal(got)
		}
	}
}

func TestP2PFirewallDoesNotTreatAccessDeniedAsMissing(t *testing.T) {
	if firewallRuleMissing(ole.NewError(0x80070005)) {
		t.Fatal("access denied cannot permit rule replacement")
	}
	if !firewallRuleMissing(ole.NewError(0x80070002)) {
		t.Fatal("missing rule not recognized")
	}
}

// Configure an unattached COM rule in memory. Never add it to Windows policy.
func TestP2PFirewallRuleProperties(t *testing.T) {
	err := withFirewall(func(_, _ *ole.IDispatch, exe string) error {
		u, err := oleutil.CreateObject("HNetCfg.FWRule")
		if err != nil {
			return err
		}
		defer u.Release()
		rule, err := u.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			return err
		}
		defer rule.Release()
		for direction := 1; direction <= 2; direction++ {
			if err := setP2PRuleProperties(rule, exe, direction); err != nil {
				return err
			}
			if firewallString(rule, "ApplicationName") != exe || firewallString(rule, "Grouping") != "Shaurma Launcher P2P" ||
				firewallInt(rule, "Profiles") != 6 || firewallInt(rule, "Direction") != int64(direction) ||
				firewallInt(rule, "Action") != 1 || firewallInt(rule, "Protocol") != 256 || firewallInt(rule, "Enabled") == 0 {
				t.Fatal("rule scope does not match installer")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
