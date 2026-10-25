// M200b (issue #3384): the .js open. QJS.BIN is console-only — ADR 0039
// fences JS off the window surfaces and this card adds no tty face — so
// the honest face is exec-to-console: the child's inherited fd-1 stream
// lands on serial beside every other app's markers, and the open names
// that sink in its decision markers rather than pretending a window got
// it. (A `--receipt=` mirror was the other candidate — the argv policy
// admits it, but the desk's share is the custom-virtio channel, where
// the receipt's createExclusive answers Unsupported: observed in the
// run-23 gate leg as a bare `Unsupported` + status=70. A sink the share
// cannot keep is a sink this card does not name.)
//
// The refusal is named BEFORE exec — a script the argv policy cannot
// take is refused with the policy's OWN error word:
//
//	AccessDenied	a .js browsed on /usb or /disk is outside the
//			/host/ confinement
//	PathLimit	the path or a component exceeds the bounds
//	InvalidPath	empty/dotted components or banned bytes
//
// A script that cannot be admitted has output that goes nowhere honest;
// exec'ing it anyway would just read the same word back off an exit
// status.
package main

import (
	"strings"

	"virelai/mime"
)

const qjsBin = "QJS.BIN"

// qjsAdmits mirrors the path() admission in tools/js-client/policy.zig —
// /host/-confined, ≤64 bytes, no NUL/backslash/colon, components ≤31 and
// never empty, "." or ".." — kept in step by hand, returning the policy's
// OWN error word (or "" when admitted) so the open names the same
// refusal QJS.BIN would, before any exec.
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

// qjsRefuse is the script-open's named refusal — the markerOpenNo shape
// the unreadable and no-handler refusals already carry.
func (m *model) qjsRefuse(name, why string) {
	m.emit(markerOpenNo + name + " (" + why + ")")
	m.status = name + ": " + why
}

// qjsQueue emits the sink half of the decision — the console is named,
// so a gate (and a reader of the log) can tell output that landed from
// output that had nowhere to go — and hands main the exec: argv is the
// bare script path ahead of the kernel's own argv[0] prepend, exactly
// the policy.parse shape.
func (m *model) qjsQueue(name, target string) {
	m.emit(markerOpenConsole + name)
	m.launch = launchReq{bin: qjsBin, path: target}
	m.status = "opening " + qjsBin
}

// qjsOpen is the Script dispatch on the default-open path: the decision
// marker pairs with the console-sink line so the run's output has a
// named home.
func (m *model) qjsOpen(name, target string) {
	if why := qjsAdmits(target); why != "" {
		m.qjsRefuse(name, why)
		return
	}
	m.emit(markerOpenFile + name + " type=" + mime.Script.String() + " handler=" + qjsBin)
	m.qjsQueue(name, target)
}

// qjsChose is the same dispatch for the open-with pick: the chose marker
// already named the pick, so only the sink line (or the refusal) is
// owed here.
func (m *model) qjsChose(name, target string) {
	if why := qjsAdmits(target); why != "" {
		m.qjsRefuse(name, why)
		return
	}
	m.qjsQueue(name, target)
}
