//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// This mode runs before loading DLLs or starting the UI. Its target is always
// this executable, never a caller-supplied path. The installer owns these rules.
func handleP2PFirewallCommand() {
	if len(os.Args) != 2 || (os.Args[1] != "--p2p-firewall-status" && os.Args[1] != "--p2p-firewall-repair") {
		return
	}
	repair := os.Args[1] == "--p2p-firewall-repair"
	err := withFirewall(func(policy, rules *ole.IDispatch, exe string) error {
		if repair {
			for direction := 1; direction <= 2; direction++ {
				if err := configureP2PRule(rules, exe, direction); err != nil {
					return err
				}
			}
			return nil
		}
		configured := true
		for direction := 1; direction <= 2; direction++ {
			v, err := oleutil.CallMethod(rules, "Item", p2pRuleName(exe, direction))
			if err != nil {
				if !firewallRuleMissing(err) {
					return err
				}
				configured = false
				continue
			}
			r := v.ToIDispatch()
			configured = configured && firewallString(r, "Grouping") == "Shaurma Launcher P2P" &&
				strings.EqualFold(firewallString(r, "ApplicationName"), exe) &&
				firewallInt(r, "Enabled") != 0 && firewallInt(r, "Action") == 1 &&
				firewallInt(r, "Direction") == int64(direction) && firewallInt(r, "Profiles")&6 == 6 &&
				firewallInt(r, "Protocol") == 256
			v.Clear()
		}
		profileValue, err := oleutil.GetProperty(policy, "CurrentProfileTypes")
		if err != nil {
			return fmt.Errorf("read firewall profiles: %w", err)
		}
		profiles := profileValue.Val
		profileValue.Clear()
		policyValue, err := oleutil.GetProperty(policy, "LocalPolicyModifyState")
		if err != nil {
			return fmt.Errorf("read firewall policy: %w", err)
		}
		policyRestricted := policyValue.Val != 0 || profiles&1 != 0
		policyValue.Clear()
		blocked := false
		err = oleutil.ForEach(rules, func(v *ole.VARIANT) error {
			defer v.Clear()
			r := v.ToIDispatch()
			if firewallInt(r, "Enabled") != 0 && firewallInt(r, "Action") == 0 &&
				firewallInt(r, "Profiles")&profiles != 0 && strings.EqualFold(firewallString(r, "ApplicationName"), exe) {
				blocked = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"supported": true, "configured": configured, "blocked": blocked,
			"policyRestricted": policyRestricted,
		})
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func withFirewall(fn func(*ole.IDispatch, *ole.IDispatch, string) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		if e, ok := err.(*ole.OleError); !ok || e.Code() != 1 {
			return err
		}
	}
	defer ole.CoUninitialize()
	u, err := oleutil.CreateObject("HNetCfg.FwPolicy2")
	if err != nil {
		return err
	}
	defer u.Release()
	p, err := u.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer p.Release()
	v, err := oleutil.GetProperty(p, "Rules")
	if err != nil {
		return err
	}
	defer v.Clear()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return fn(p, v.ToIDispatch(), exe)
}

func firewallString(d *ole.IDispatch, name string) string {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return ""
	}
	defer v.Clear()
	return v.ToString()
}

func firewallInt(d *ole.IDispatch, name string) int64 {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return -1
	}
	defer v.Clear()
	return v.Val
}

func p2pRuleName(exe string, direction int) string {
	return fmt.Sprintf("Shaurm Launcher P2P-%s-%d", filepath.Base(exe), direction)
}

func configureP2PRule(rules *ole.IDispatch, exe string, direction int) error {
	name := p2pRuleName(exe, direction)
	v, err := oleutil.CallMethod(rules, "Item", name)
	if err != nil && !firewallRuleMissing(err) {
		return err
	}
	var rule *ole.IDispatch
	create := err != nil
	if !create {
		defer v.Clear()
		rule = v.ToIDispatch()
		if firewallString(rule, "Grouping") != "Shaurma Launcher P2P" || !strings.EqualFold(firewallString(rule, "ApplicationName"), exe) {
			return fmt.Errorf("firewall rule name belongs to another application")
		}
	} else {
		u, err := oleutil.CreateObject("HNetCfg.FWRule")
		if err != nil {
			return err
		}
		defer u.Release()
		rule, err = u.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			return err
		}
		defer rule.Release()
	}
	if err := setP2PRuleProperties(rule, exe, direction); err != nil {
		return err
	}
	if create {
		v, err := oleutil.CallMethod(rules, "Add", rule)
		if err != nil {
			return err
		}
		v.Clear()
	}
	return nil
}

func setP2PRuleProperties(rule *ole.IDispatch, exe string, direction int) error {
	for _, field := range []struct {
		name  string
		value any
	}{
		{"Name", p2pRuleName(exe, direction)}, {"Grouping", "Shaurma Launcher P2P"}, {"ApplicationName", exe},
		{"Protocol", 256}, {"Direction", direction}, {"Profiles", 6}, {"Action", 1}, {"Enabled", true},
	} {
		v, err := oleutil.PutProperty(rule, field.name, field.value)
		if err != nil {
			return err
		}
		v.Clear()
	}
	return nil
}

// A COM access/service error is not proof that a rule is absent. Never replace
// a potentially unrelated rule after an unsuccessful ownership check.
func firewallRuleMissing(err error) bool {
	var e *ole.OleError
	if !errors.As(err, &e) {
		return false
	}
	if uint32(e.Code()) == 0x80070002 {
		return true
	} // HRESULT_FROM_WIN32(ERROR_FILE_NOT_FOUND)
	if sub, ok := e.SubError().(interface{ SCODE() uint32 }); ok {
		return sub.SCODE() == 0x80070002
	}
	return false
}
