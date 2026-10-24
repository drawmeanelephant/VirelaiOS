// M200b (issue #3384): the .js open. QJS.BIN is console-only — ADR 0039
// fences JS off the window surfaces and this card adds no tty face — so
// the honest exec-to-console face is TWO sinks, both named at dispatch:
//
//	console	the child's inherited fd-1 stream lands on serial beside
//		every other app's markers — the host-observable half
//	receipt	the argv policy's own --receipt=/host/NAME mirror
//		(tools/js-client/policy.zig) writes every 'O' output frame,
//		'D' diagnostic and the final status row into a share file
//		beside the script — the desk-visible half a bound tty
//		would have been
//
// The refusals are named BEFORE exec — a launch that cannot run, or
// cannot keep its record, is worse than no launch:
//
//	(AccessDenied / PathLimit / InvalidPath)
//				the argv policy admits /host/-confined paths
//				inside tight bounds only; a .js browsed on
//				/usb or /disk, or under a component it cannot
//				take, is a script QJS.BIN refuses at parse —
//				the open names the policy's own error first
//	(no receipt name)	no <stem>.R / <stem>-N.R inside the path
//				policy's bounds is free — the run's output
//				has nowhere honest to go
package main

import (
	"strings"

	"virelai/mime"
)

const qjsBin = "QJS.BIN"

// qjsReceiptTries bounds the free-name search: <stem>.R first, then
// <stem>-1.R … <stem>-9.R — the digits that exist, like maxCandidates.
const qjsReceiptTries = 10

// qjsAdmits mirrors the path() admission in tools/js-client/policy.zig —
// /host/-confined, ≤64 bytes, no NUL/backslash/colon, components ≤31 and
// never empty, "." or ".." — kept in step by hand, returning the policy's
// OWN error word (or "" when admitted) so the open names the same
// refusal QJS.BIN would, before any exec. A derived receipt name must
// pass it too: a path the policy would refuse at parse is not a name.
func qjsAdmits(p string) string {
	if !strings.HasPrefix(p, "/host/") {
		return "AccessDenied"
	}
	if len(p) > 64 {
		return "PathLimit"
	}
	if strings.IndexAny(p, "\x00\\:") >= 0 {
		return "InvalidPath"
	}
	for _, part := range strings.Split(p[6:], "/") {
		if len(part) == 0 || part == "." || part == ".." {
			return "InvalidPath"
		}
		if len(part) > 31 {
			return "PathLimit"
		}
	}
	return ""
}

// qjsLaunch shapes the exec for a Script open. The receipt name is the
// first FREE <dir>/<stem>.R / <stem>-N.R inside the policy's bounds —
// free is the readHead seam's answer, the share refusing the read being
// how the model learns a name is unclaimed (QJS.BIN's createExclusive
// then cannot lose a race the probe already named).
func qjsLaunch(target string) (launchReq, string, string) {
	if why := qjsAdmits(target); why != "" {
		return launchReq{}, "", why
	}
	slash := strings.LastIndexByte(target, '/')
	base := target[slash+1:]
	stem := base
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		stem = base[:i]
	}
	for i := 0; i < qjsReceiptTries; i++ {
		name := stem + ".R"
		if i > 0 {
			name = stem + "-" + string(rune('0'+i)) + ".R"
		}
		rcpt := target[:slash] + "/" + name
		if qjsAdmits(rcpt) != "" {
			continue
		}
		if _, rc := readHead(rcpt, 1); rc < 0 {
			return launchReq{
				bin:  qjsBin,
				path: target,
				args: []string{"--receipt=" + rcpt},
			}, rcpt, ""
		}
	}
	return launchReq{}, "", "no receipt name"
}

// qjsRefuse is the script-open's named refusal — the markerOpenNo shape
// the unreadable and no-handler refusals already carry.
func (m *model) qjsRefuse(name, why string) {
	m.emit(markerOpenNo + name + " (" + why + ")")
	m.status = name + ": " + why
}

// qjsQueue emits the receipt half of the decision and hands main the
// exec — argv = [--receipt=<derived>, <script>] ahead of the kernel's
// own argv[0] prepend, exactly the policy.parse shape.
func (m *model) qjsQueue(name string, lr launchReq, rcpt string) {
	m.emit(markerOpenReceipt + name + " -> " + rcpt)
	m.launch = lr
	m.status = "opening " + qjsBin
}

// qjsOpen is the Script dispatch on the default-open path: the decision
// marker pairs with the receipt line so a gate (and the desk) can tell
// where the run's output was meant to land.
func (m *model) qjsOpen(name, target string) {
	lr, rcpt, why := qjsLaunch(target)
	if why != "" {
		m.qjsRefuse(name, why)
		return
	}
	m.emit(markerOpenFile + name + " type=" + mime.Script.String() + " handler=" + qjsBin)
	m.qjsQueue(name, lr, rcpt)
}

// qjsChose is the same dispatch for the open-with pick: the chose marker
// already named the pick, so only the receipt line (or the refusal) is
// owed here.
func (m *model) qjsChose(name, target string) {
	lr, rcpt, why := qjsLaunch(target)
	if why != "" {
		m.qjsRefuse(name, why)
		return
	}
	m.qjsQueue(name, lr, rcpt)
}
