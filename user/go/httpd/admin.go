// admin.go — M209a (issue #3825, index #3824): GOHTTPD's opt-in, read-only
// /admin/ section — the same syscall readers GOTOP draws (processes, net
// counters, mounts, grants + denials, GPU compute load) rendered as plain
// no-JS HTML tables (ADR 0028 D2 / ADR 0039 §1) so WEB.ELF reads them.
//
// The section is locked by default in three layers, checked in order:
// TLS first (a plaintext request is refused 403 by name), then the
// `httpd-admin` secret-store row (no row → the section does not exist,
// 404), then HTTP Basic credentials presented as `admin:<row value>`
// (mismatch → 401 with a Basic challenge). Credential bytes live only in
// wiped buffers and never reach a marker or the serial log.
//
// A reader that refuses renders "unknown (<errno>)", never an empty table
// that would read as "no rows" (the card's "unknown is a value" rule).
// This file imports virelai/vi + friends; server.go stays ABI-free.
package main

import (
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"virelai/gpu3d"
	"virelai/shlib"
	"virelai/vi"
)

// adminEnabled is set from --admin in main; adminSecure is set by
// serveConn to whether the connection it answers rides TLS. The server
// answers connections serially, so one pair of words is the whole state.
var (
	adminEnabled bool
	adminSecure  bool
)

// adminKey is the secret-store row Basic auth is checked against;
// adminUser is the fixed Basic user name the pair is presented as.
const (
	adminKey  = "httpd-admin"
	adminUser = "admin"
)

// The guest readers, as seams: vi/shlib/gpu3d on the guest, stubs in host
// tests (the same discipline GOTOP's mountRows/gpuSampleStats keep).
var (
	adminStoreGet = shlib.StoreGet
	adminProcs    = func() ([]vi.ProcRow, int64) {
		dst := make([]vi.ProcRow, 64)
		n, rc := vi.ProcsV2(dst)
		if rc < 0 {
			return nil, rc
		}
		return dst[:n], rc
	}
	adminNetStats = vi.NetStatsSnapshotRC
	adminMounts   = vi.Mounts
	adminGrants   = vi.GrantRows
	adminDenials  = vi.GrantDenials
	adminGPU      = gpu3d.SampleStats
	adminNow      = vi.Time
)

// isAdminPath names the /admin/ subtree — "/admin" itself or anything
// under "/admin/". "/administrator" is an ordinary share path.
func isAdminPath(p string) bool {
	return p == "/admin" || strings.HasPrefix(p, "/admin/")
}

// routeAdmin answers an /admin/ request under --admin. The checks run in
// the card's order: the connection must be TLS, the store must carry an
// httpd-admin row, and the request's Basic credentials must match it.
func routeAdmin(req request) response {
	if !adminSecure {
		return response{Status: 403, ContentType: "text/plain; charset=utf-8",
			Body: []byte("admin requires tls\n")}
	}
	var val [vi.SecretValMax]byte
	n := adminStoreGet(adminKey, val[:])
	if n < 0 {
		wipeBytes(val[:])
		return notFound()
	}
	ok := adminCredsOK(req.Authorization, val[:n])
	wipeBytes(val[:])
	if !ok {
		return response{Status: 401, ContentType: "text/plain; charset=utf-8",
			Body:  []byte("unauthorized\n"),
			Extra: []string{`WWW-Authenticate: Basic realm="admin"`}}
	}
	switch req.Path {
	case "/admin", "/admin/":
		return adminPage("index", adminIndexBody())
	case "/admin/procs":
		return adminPage("procs", adminProcsBody())
	case "/admin/net":
		return adminPage("net", adminNetBody())
	case "/admin/mounts":
		return adminPage("mounts", adminMountsBody())
	case "/admin/grants":
		return adminPage("grants", adminGrantsBody())
	case "/admin/gpu":
		return adminPage("gpu", adminGPUBody())
	}
	return notFound()
}

// adminCredsOK checks an `Authorization: Basic <b64>` header against the
// stored row: the decoded pair must be "admin:<secret>" exactly, compared
// in constant time. The decoded bytes are wiped before returning.
func adminCredsOK(header string, secret []byte) bool {
	const scheme = "basic "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[len(scheme):]))
	if err != nil {
		return false
	}
	defer wipeBytes(raw)
	want := append([]byte(adminUser+":"), secret...)
	defer wipeBytes(want)
	return subtle.ConstantTimeCompare(raw, want) == 1
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// -------------------------------------------------------------------------
// Pages
// -------------------------------------------------------------------------

var htmlEsc = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
)

func esc(s string) string { return htmlEsc.Replace(s) }

// adminPage wraps one section body in the page chrome: the heading, the
// sample timestamp, and the link back to the index.
func adminPage(name string, inner []byte) response {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><title>admin: " + esc(name) +
		"</title></head>\n<body><h1>admin: " + esc(name) + "</h1>\n")
	b.WriteString("<p>sampled " + adminStamp() + "</p>\n")
	b.Write(inner)
	if name != "index" {
		b.WriteString("<p><a href=\"/admin/\">index</a></p>\n")
	}
	b.WriteString("</body></html>\n")
	return response{Status: 200, ContentType: "text/html; charset=utf-8", Body: []byte(b.String())}
}

// adminStamp is the page's timestamp cell: the kernel's epoch seconds, or
// "t unknown" when the clock refused.
func adminStamp() string {
	if t := adminNow(); t > 0 {
		return "t=" + strconv.FormatInt(t, 10)
	}
	return "t unknown"
}

// tableHTML renders a plain <table>: one <th> row, then <td> rows.
// Every cell passes esc — grant names and receipt lines are kernel text,
// not markup.
func tableHTML(headers []string, rows [][]string) []byte {
	var b strings.Builder
	b.WriteString("<table>\n<tr>")
	for _, h := range headers {
		b.WriteString("<th>" + esc(h) + "</th>")
	}
	b.WriteString("</tr>\n")
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range r {
			b.WriteString("<td>" + esc(c) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n")
	return []byte(b.String())
}

// unknownP renders a refused reader as "<name> unknown (<errno>)" —
// "unknown is a value", never an empty table posed as "no rows".
func unknownP(name string, rc int64) []byte {
	n := vi.ErrnoName(rc)
	if n == "" {
		n = "refused"
	}
	return []byte("<p>" + esc(name) + " unknown (" + esc(n) + ")</p>\n")
}

func adminIndexBody() []byte {
	return []byte("<ul>\n" +
		"<li><a href=\"/admin/procs\">procs</a></li>\n" +
		"<li><a href=\"/admin/net\">net</a></li>\n" +
		"<li><a href=\"/admin/mounts\">mounts</a></li>\n" +
		"<li><a href=\"/admin/grants\">grants</a></li>\n" +
		"<li><a href=\"/admin/gpu\">gpu</a></li>\n" +
		"</ul>\n")
}

// procStateName is GOTOP's state vocabulary (top/table.go StateName).
func procStateName(state uint64) string {
	switch state {
	case vi.ProcCreated:
		return "created"
	case vi.ProcRunning:
		return "running"
	case vi.ProcExited:
		return "exited"
	}
	return "unknown"
}

func adminProcsBody() []byte {
	rows, rc := adminProcs()
	if rc < 0 {
		return unknownP("procs", rc)
	}
	var trs [][]string
	for _, p := range rows {
		trs = append(trs, []string{
			strconv.FormatUint(p.PID, 10),
			procStateName(p.State),
			strconv.FormatUint(p.ExitStatus, 10),
			p.Name(),
			strconv.FormatUint(p.CpuTicks, 10),
			strconv.FormatUint(p.MemBytes, 10),
		})
	}
	return tableHTML(
		[]string{"pid", "state", "exit", "name", "cpu_ticks", "mem_bytes"}, trs)
}

func adminNetBody() []byte {
	s, rc := adminNetStats()
	if rc != vi.NetStatsBytes {
		return unknownP("net", rc)
	}
	ip := func(a [4]byte) string {
		return strconv.Itoa(int(a[0])) + "." + strconv.Itoa(int(a[1])) +
			"." + strconv.Itoa(int(a[2])) + "." + strconv.Itoa(int(a[3]))
	}
	u32 := func(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
	u64 := func(v uint64) string { return strconv.FormatUint(v, 10) }
	var mac strings.Builder
	for i, b := range s.MAC {
		if i > 0 {
			mac.WriteByte(':')
		}
		const hex = "0123456789abcdef"
		mac.WriteByte(hex[b>>4])
		mac.WriteByte(hex[b&0xf])
	}
	return tableHTML([]string{"counter", "value"}, [][]string{
		{"mac", mac.String()},
		{"own_ip", ip(s.OwnIP)},
		{"gateway", ip(s.Gateway)},
		{"dhcp_state", u32(uint32(s.DHCPState))},
		{"lease_ip", ip(s.LeaseIP)},
		{"lease_secs", u32(s.LeaseSecs)},
		{"tcp_state", u32(uint32(s.TCPState))},
		{"tcp_peer", ip(s.TCPPeerIP) + ":" + u32(uint32(s.TCPPeerPort))},
		{"udp_count", u32(uint32(s.UDPCount))},
		{"arp_count", u32(uint32(s.ARPCount))},
		{"tx_frames", u64(s.TXFrames)},
		{"tx_bytes", u64(s.TXBytes)},
		{"rx_frames", u64(s.RXFrames)},
		{"rx_bytes", u64(s.RXBytes)},
		{"rx_filtered", u64(s.RXFiltered)},
		{"rx_overflow", u64(s.RXOverflow)},
	})
}

func adminMountsBody() []byte {
	ms, rc := adminMounts()
	if rc < 0 {
		return unknownP("mounts", rc)
	}
	var trs [][]string
	for _, m := range ms {
		flags := m.AccessName()
		if m.Sealed {
			flags += "+sealed"
		}
		trs = append(trs, []string{m.Name, m.KindName(), m.StateName(), flags})
	}
	return tableHTML([]string{"name", "kind", "state", "flags"}, trs)
}

// adminGrantsBody is the grants page: the GRANTS.TXT user rows, then the
// kernel's denial-receipt ring (drained from cursor 0 each request). Each
// reader renders its own unknown — a refused walk never paints as "no
// grants".
func adminGrantsBody() []byte {
	var b []byte
	rows, rc := adminGrants()
	if rc < 0 {
		b = append(b, unknownP("grants", rc)...)
	} else {
		var trs [][]string
		for _, r := range rows {
			trs = append(trs, []string{r.Name, r.SHA, r.Words})
		}
		b = append(b, tableHTML([]string{"name", "sha256", "words"}, trs)...)
	}
	b = append(b, "<h2>denials</h2>\n"...)
	lines, _, dropped, drc := adminDenials(0)
	if drc < 0 {
		b = append(b, unknownP("denials", drc)...)
	} else {
		if dropped > 0 {
			b = append(b, "<p>dropped="+strconv.FormatUint(dropped, 10)+"</p>\n"...)
		}
		var trs [][]string
		for _, l := range lines {
			trs = append(trs, []string{l})
		}
		b = append(b, tableHTML([]string{"receipt"}, trs)...)
	}
	return b
}

// adminErrToken names a gpu3d refusal with GOTOP's marker vocabulary
// (gpuview.go gpuErrText).
func adminErrToken(err error) string {
	switch {
	case err == gpu3d.ErrComputeNoDevice:
		return "no-device"
	case err == gpu3d.ErrComputeExhausted:
		return "exhausted"
	case err == gpu3d.ErrComputeBusy:
		return "busy"
	case err == gpu3d.ErrComputeInvalid:
		return "invalid"
	case err == gpu3d.ErrComputeDevice:
		return "device-fault"
	case err == gpu3d.ErrComputeTimedOut:
		return "timeout"
	case err == gpu3d.ErrComputeBadHandle:
		return "bad-handle"
	case err == gpu3d.ErrComputeLost:
		return "lost"
	case err == gpu3d.ErrComputeFault:
		return "fault"
	default:
		return "error"
	}
}

func adminGPUBody() []byte {
	st, err := adminGPU()
	if err != nil {
		return []byte("<p>gpu unknown (" + esc(adminErrToken(err)) + ")</p>\n")
	}
	u32 := func(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
	u64 := func(v uint64) string { return strconv.FormatUint(v, 10) }
	return tableHTML([]string{"counter", "value"}, [][]string{
		{"busy", u32(st.Busy)},
		{"queued", u32(st.Queued)},
		{"sessions", u32(st.SessionsLive)},
		{"last_kernel_us", u64(st.LastKernelUs)},
		{"total_kernel_us", u64(st.TotalKernelUs)},
		{"submits", u64(st.Submits)},
		{"completed", u64(st.Completed)},
		{"refusals", u64(st.Refusals)},
		{"timeouts", u64(st.Timeouts)},
	})
}
