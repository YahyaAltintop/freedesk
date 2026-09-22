//go:build windows

package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// query prints, in the shape parse reads: how many firewall profiles are on,
// what kind of network the computer is on, and one line per enabled inbound
// rule that names a program. The rules and their program filters are two
// listings joined on InstanceID rather than one call per rule: a machine has
// hundreds of rules, and a call per rule takes half a minute.
const query = "$ErrorActionPreference = 'SilentlyContinue'; " +
	"'enabled|' + @(Get-NetFirewallProfile | Where-Object { $_.Enabled -eq 'True' }).Count; " +
	"'network|' + (@(Get-NetConnectionProfile | Select-Object -ExpandProperty NetworkCategory) -join ','); " +
	"$rules = @{}; " +
	"Get-NetFirewallRule -Direction Inbound -Enabled True | ForEach-Object { $rules[$_.InstanceID] = '' + $_.Action + '|' + $_.Profile }; " +
	"Get-NetFirewallApplicationFilter | ForEach-Object { $r = $rules[$_.InstanceID]; if ($r) { 'rule|' + $r + '|' + $_.Program } }"

// Check asks Windows Defender Firewall what it would do with packets for exe
// from another computer. It runs PowerShell, takes a few seconds, and belongs
// in the background.
func Check(ctx context.Context, exe string) (Status, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", query)
	// The agent has no console of its own; without this PowerShell would put
	// one on the operator's screen.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.Output()
	if err != nil {
		return Status{}, fmt.Errorf("could not query the firewall: %w", err)
	}
	text := string(out)
	if !strings.Contains(text, "enabled|") {
		return Status{}, fmt.Errorf("the firewall query answered nothing usable: %q", strings.TrimSpace(text))
	}
	return parse(text, exe), nil
}
