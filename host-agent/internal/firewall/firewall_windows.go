//go:build windows

package firewall

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"
)

// Firewall profiles and the other values the answers are made of, from
// icftypes.h and netlistmgr.h.
const (
	profileDomain  = 0x1 // NET_FW_PROFILE2_DOMAIN
	profilePrivate = 0x2 // NET_FW_PROFILE2_PRIVATE
	profilePublic  = 0x4 // NET_FW_PROFILE2_PUBLIC
	profileAll     = 0x7fffffff

	ruleDirIn            = 1 // NET_FW_RULE_DIR_IN
	actionBlock          = 0 // NET_FW_ACTION_BLOCK
	actionAllow          = 1 // NET_FW_ACTION_ALLOW
	ruleCategoryFirewall = 2 // NET_FW_RULE_CATEGORY_FIREWALL

	nlmEnumNetworkConnected = 0x1

	// Bounds on lists that are short on any real machine.
	maxProducts = 16
	maxNetworks = 64
)

// Check asks Windows Defender Firewall what it would do with packets for exe
// from another computer, or which product it has handed that question to.
// The answers come from the Windows Firewall API and the Network List
// Manager, in this process: a few milliseconds, no other program started.
// They are put into the line format parse reads (see query), so judging
// them is one procedure, tested on its own.
func Check(ctx context.Context, exe string) (Status, error) {
	report, err := ask(ctx, false)
	if err != nil {
		return Status{}, fmt.Errorf("could not query the firewall: %w", err)
	}
	return parse(report, exe), nil
}

// ask runs query on a goroutine of its own. COM is set up per OS thread, so
// the goroutine keeps its thread and, never unlocking it, takes the thread
// along when it ends, with whatever COM left on it. A call that hangs inside
// Windows cannot be interrupted: ctx only stops the waiting for it.
func ask(ctx context.Context, allRules bool) (string, error) {
	type answer struct {
		report string
		err    error
	}
	done := make(chan answer, 1) // the goroutine never waits for a caller that has left
	go func() {
		runtime.LockOSThread()
		report, err := query(ctx, allRules)
		done <- answer{report, err}
	}()
	select {
	case a := <-done:
		return a.report, a.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// query writes what Windows says, a line each, in the format parse reads:
// which product has registered with Windows to run the firewall (empty when
// none has), how many of the firewall's three profiles are on, the
// categories of the networks the computer is connected to and, unless a
// product runs the firewall (then Windows' own rules are not enforced) or
// allRules is set, one line per enabled inbound rule that names a program.
// It must run on a locked OS thread.
func query(ctx context.Context, allRules bool) (report string, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic while reading the firewall: %v", p)
		}
	}()
	r, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	switch uint32(r) {
	case sOK, sFalse:
		defer func() { _, _, _ = procCoUninitialize.Call() }()
	case rpcEChangedMode:
		// The thread already runs COM in the other threading model, which
		// serves these objects as well.
	default:
		return "", hresultErr("CoInitializeEx", r)
	}

	var b strings.Builder
	owner := firewallOwner()
	fmt.Fprintf(&b, "owner|%s\n", owner)

	policy, err := create(&clsidNetFwPolicy2, &iidINetFwPolicy2)
	if err != nil {
		return "", fmt.Errorf("HNetCfg.FwPolicy2: %w", err)
	}
	defer release(policy)
	on, err := enabledProfiles(policy)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "enabled|%d\n", on)
	fmt.Fprintf(&b, "network|%s\n", strings.Join(connectedNetworks(), ","))
	if owner == "" || allRules {
		if err := inboundRules(ctx, policy, &b); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// firewallOwner names the first product registered with Windows for the
// firewall rule category, the one that runs the firewall in Windows' place,
// fit for one line. It returns "" when there is none, and when the register
// cannot be read (Windows Security Center off, Windows Server): Windows' own
// rules are then what counts, as far as can be told.
func firewallOwner() string {
	products, err := create(&clsidNetFwProducts, &iidINetFwProducts)
	if err != nil {
		return ""
	}
	defer release(products)
	count, err := getInt32(products, productsCount)
	if err != nil {
		return ""
	}
	for i := range min(count, maxProducts) {
		var product unsafe.Pointer
		r, _, _ := syscall.SyscallN(method(products, productsItem), uintptr(products), uintptr(i), uintptr(unsafe.Pointer(&product)))
		if failed(r) || product == nil {
			continue
		}
		name := firewallProduct(product)
		release(product)
		if name != "" {
			return name
		}
	}
	return ""
}

// firewallProduct returns the display name of product, fit for one line, if
// it is registered for the firewall rule category, and "" otherwise.
func firewallProduct(product unsafe.Pointer) string {
	var categories variant
	r, _, _ := syscall.SyscallN(method(product, productRuleCategories), uintptr(product), uintptr(unsafe.Pointer(&categories)))
	if failed(r) {
		return ""
	}
	runsFirewall := slices.Contains(categories.ints(), ruleCategoryFirewall)
	categories.clear()
	if !runsFirewall {
		return ""
	}
	name, err := getString(product, productDisplayName)
	if err != nil {
		return ""
	}
	return cleanName(name)
}

// enabledProfiles counts the firewall profiles (domain, private, public)
// that are on.
func enabledProfiles(policy unsafe.Pointer) (int, error) {
	on := 0
	for _, profile := range []uintptr{profileDomain, profilePrivate, profilePublic} {
		var enabled int16 // VARIANT_BOOL
		r, _, _ := syscall.SyscallN(method(policy, policy2FirewallEnabled), uintptr(policy), profile, uintptr(unsafe.Pointer(&enabled)))
		if failed(r) {
			return 0, hresultErr("FirewallEnabled", r)
		}
		if enabled != 0 {
			on++
		}
	}
	return on, nil
}

// connectedNetworks lists the category of each network the computer is
// connected to, by the names parse reads, or nothing when the Network List
// Manager cannot say. These categories decide which profile's rules a
// viewer's packets meet. The firewall's own list of active profiles is no
// substitute: it can name a profile that no connected network has (Public
// beside Private on a computer with Hyper-V, whose networks were all
// Private).
func connectedNetworks() []string {
	nlm, err := create(&clsidNetworkListManager, &iidINetworkListManager)
	if err != nil {
		return nil
	}
	defer release(nlm)
	var networks unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(nlm, nlmGetNetworks), uintptr(nlm), nlmEnumNetworkConnected, uintptr(unsafe.Pointer(&networks)))
	if failed(r) || networks == nil {
		return nil
	}
	defer release(networks)
	var names []string
	for range maxNetworks {
		var network unsafe.Pointer
		var fetched uint32
		r, _, _ := syscall.SyscallN(method(networks, enumNetworksNext), uintptr(networks), 1,
			uintptr(unsafe.Pointer(&network)), uintptr(unsafe.Pointer(&fetched)))
		if failed(r) || fetched == 0 || network == nil {
			break
		}
		category, err := getInt32(network, networkGetCategory)
		release(network)
		if err != nil {
			continue
		}
		if name := categoryName(category); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// categoryName spells an NLM_NETWORK_CATEGORY the way parse reads it.
func categoryName(category int32) string {
	switch category {
	case 0:
		return "Public"
	case 1:
		return "Private"
	case 2:
		return "DomainAuthenticated"
	}
	return ""
}

// inboundRules writes a "rule|Action|Profiles|Program" line for every
// enabled inbound rule of the computer's own firewall policy that names a
// program. A machine has hundreds of rules; reading them takes milliseconds.
func inboundRules(ctx context.Context, policy unsafe.Pointer, b *strings.Builder) error {
	var rules unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(policy, policy2Rules), uintptr(policy), uintptr(unsafe.Pointer(&rules)))
	if failed(r) {
		return hresultErr("Rules", r)
	}
	if rules == nil {
		return errors.New("Rules: no collection")
	}
	defer release(rules)
	var unknown unsafe.Pointer
	r, _, _ = syscall.SyscallN(method(rules, rulesNewEnum), uintptr(rules), uintptr(unsafe.Pointer(&unknown)))
	if failed(r) {
		return hresultErr("Rules._NewEnum", r)
	}
	if unknown == nil {
		return errors.New("Rules._NewEnum: no enumerator")
	}
	each, err := queryInterface(unknown, &iidIEnumVARIANT)
	release(unknown)
	if err != nil {
		return err
	}
	defer release(each)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var v variant
		var fetched uint32
		r, _, _ := syscall.SyscallN(method(each, enumVariantNext), uintptr(each), 1,
			uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&fetched)))
		if failed(r) {
			return hresultErr("IEnumVARIANT::Next", r)
		}
		if fetched == 0 {
			return nil
		}
		writeRule(b, &v)
		v.clear()
	}
}

// writeRule writes the line for the rule v holds, if it is an enabled
// inbound rule that names a program. A rule whose properties cannot be read
// is passed over, like one that names no program.
func writeRule(b *strings.Builder, v *variant) {
	if (v.vt != vtDispatch && v.vt != vtUnknown) || v.val == nil {
		return
	}
	rule, err := queryInterface(v.val, &iidINetFwRule)
	if err != nil {
		return
	}
	defer release(rule)
	if dir, err := getInt32(rule, ruleDirection); err != nil || dir != ruleDirIn {
		return
	}
	if enabled, err := getBool(rule, ruleEnabled); err != nil || !enabled {
		return
	}
	var verb string
	switch action, err := getInt32(rule, ruleAction); {
	case err != nil:
		return
	case action == actionBlock:
		verb = "Block"
	case action == actionAllow:
		verb = "Allow"
	default:
		return
	}
	profiles, err := getInt32(rule, ruleProfiles)
	if err != nil {
		return
	}
	program, err := getString(rule, ruleApplicationName)
	if err != nil || program == "" || strings.ContainsAny(program, "\r\n") {
		return
	}
	fmt.Fprintf(b, "rule|%s|%s|%s\n", verb, profileNames(profiles), program)
}

// profileNames spells a rule's profile bits the way parse reads them: "Any"
// for every profile, otherwise the ones set, as in "Private, Public".
func profileNames(bits int32) string {
	if bits == profileAll {
		return "Any"
	}
	var names []string
	for _, p := range []struct {
		bit  int32
		name string
	}{{profileDomain, "Domain"}, {profilePrivate, "Private"}, {profilePublic, "Public"}} {
		if bits&p.bit != 0 {
			names = append(names, p.name)
		}
	}
	return strings.Join(names, ", ")
}
