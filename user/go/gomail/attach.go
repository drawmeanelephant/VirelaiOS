package main

// attach.go — M208b (issue #3823): the attachment half. A fetched
// attachment lands under the data root's MAIL/ subtree with a sanitized
// name and an ATTACH index row (box/seq/saved/type/origin-name); `open
// <name>` then routes the saved file through the same opens= contract
// the seat and GOFILES share — APPS.TXT decoded, opensAccepts applied
// to the part's declared mime, the accepting row's bin+argv exec'd with
// the path last (the files/manifest.go + gotabwm/drag.go rule, third
// reader of the documented grammar). text/calendar lands on GOCLOCK's
// headless `import` door, text/vcard on GOFILES' contact face; a type
// no manifest row accepts — or a name the index never saved — is a
// named refusal. The file itself is NEVER executed: the only exec line
// this file produces is `<handler bin> [argv…] <saved path>`.
//
// Markers (grep-able, one per line):
//
//	gomail: attach <seq> <name> <type>   the listing row (unchanged)
//	gomail: saved <path>                 the write landed
//	gomail: attach-fail <name> <why>     fetch/decode/bound/write miss
//	gomail: open <name> handler=<bin>    the accepted hand-off
//	gomail: refuse open <name> [<type>]  unknown name or unrouted type
//	gomail: refuse open <name> <err>     the exec itself refused

import (
	"strings"

	"virelai/vi"
)

const (
	attachNameMax   = 48            // one saved basename's bound (the drop rule)
	attachBytesMax  = 224 * 1024    // decoded payload bound — under literalMax
	attachPerMsgMax = 8             // parts one message may save
	attachIndexMax  = 64            // ATTACH rows kept, oldest evicted first
	attachAppsPath  = "/host/APPS.TXT"
	attachAppsMax   = 8192 // every manifest reader's budget (manifest_buf_max)
)

// attExec is the exec seam — vi.Exec on the guest, a stub in tests (the
// files execApp pattern: one named call, a refusal marker can never
// claim a launch that did not happen).
var attExec = vi.Exec

// attAppsRead is the manifest read seam — vi.ReadFileAll on the guest.
var attAppsRead = vi.ReadFileAll

// sanitizeAttachName renders one attachment name safe under MAIL/: the
// basename of the declared name (any '/' or '\' run drops), each byte
// outside [A-Za-z0-9._-] folded to '_', leading non-alphanumerics
// trimmed (a name can never become ".", ".." or a dotfile), and the
// whole bounded at attachNameMax bytes. An empty result is "attach" —
// the name is a label, not a path, so folding beats refusing.
func sanitizeAttachName(name string) string {
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	b := []byte(name)
	for i, c := range b {
		ok := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' ||
			c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			b[i] = '_'
		}
	}
	s := string(b)
	s = strings.TrimLeft(s, "._-")
	if len(s) > attachNameMax {
		s = s[:attachNameMax]
	}
	if s == "" {
		s = "attach"
	}
	return s
}

// attachExtFor is the extension a routed subtype must carry on its
// saved name: the hand-off exec lines are the opens= contract's own
// shape (`GOCLOCK.ELF <path>.ics`, `GOFILES.ELF <path>.vcf`), and both
// targets admit by suffix — GOCLOCK's verbArg and GOFILES' isVCFPath.
func attachExtFor(mt string) string {
	switch lowerASCII(mt) {
	case "text/calendar":
		return ".ics"
	case "text/vcard":
		return ".vcf"
	}
	return ""
}

// b64decode reverses smtp.go's b64encode alphabet: whitespace ignored
// (MIME folds at 76), '=' terminates, a bad byte or a truncated tail
// fails the whole decode — a half-decoded payload never saves.
func b64decode(s []byte) ([]byte, bool) {
	var rev [256]byte
	for i := range rev {
		rev[i] = 0xff
	}
	for i := 0; i < len(b64alpha); i++ {
		rev[b64alpha[i]] = byte(i)
	}
	var out []byte
	var w uint32
	nb := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '=' {
			break
		}
		v := rev[c]
		if v == 0xff {
			if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
				continue
			}
			return nil, false
		}
		w = w<<6 | uint32(v)
		nb++
		if nb == 4 {
			out = append(out, byte(w>>16), byte(w>>8), byte(w))
			w, nb = 0, 0
		}
	}
	if nb == 3 {
		out = append(out, byte(w>>10), byte(w>>2))
	} else if nb == 2 {
		out = append(out, byte(w>>4))
	} else if nb == 1 {
		return nil, false // one trailing sextet is not a byte
	}
	return out, true
}

// --- the ATTACH index -------------------------------------------------
//
// One file under MAIL/: `box \t seq \t saved \t type \t origname` per
// row, the HDRS field grammar. `saved` is the sanitized basename the
// payload wrote to, `origname` the name the wire declared.

type attachRow struct {
	box   string
	seq   int
	saved string
	mt    string
	orig  string
}

func attachIndex() []attachRow {
	var out []attachRow
	for _, line := range strings.Split(string(cacheRead("ATTACH")), "\n") {
		if line == "" {
			continue
		}
		f := fields(line)
		if len(f) != 5 {
			continue
		}
		n, ok := parseUint(f[1])
		if !ok || n <= 0 {
			continue
		}
		out = append(out, attachRow{
			box: f[0], seq: int(n), saved: f[2], mt: f[3], orig: f[4],
		})
	}
	return out
}

func attachIndexWrite(rows []attachRow) {
	for len(rows) > attachIndexMax {
		rows = rows[1:]
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.box)
		b.WriteByte('\t')
		b.WriteString(itoa(int64(r.seq)))
		b.WriteByte('\t')
		b.WriteString(r.saved)
		b.WriteByte('\t')
		b.WriteString(r.mt)
		b.WriteByte('\t')
		b.WriteString(r.orig)
		b.WriteByte('\n')
	}
	cacheWrite("ATTACH", []byte(b.String()))
}

// attachFor lists the indexed rows of one message — the window's
// attachment rows for the picked envelope.
func attachFor(box string, seq int) []attachRow {
	var out []attachRow
	for _, r := range attachIndex() {
		if r.box == box && r.seq == seq {
			out = append(out, r)
		}
	}
	return out
}

// attachFind resolves an `open` argument against the index: the
// declared name first (what the marker listed), the saved basename
// second — never a path, never a substring.
func attachFind(name string) (attachRow, bool) {
	rows := attachIndex()
	for _, r := range rows {
		if r.orig == name {
			return r, true
		}
	}
	for _, r := range rows {
		if r.saved == name {
			return r, true
		}
	}
	return attachRow{}, false
}

// --- save -------------------------------------------------------------

// attachSaveName picks the on-share basename for one part: the
// sanitized declared name, the routed subtype's extension ensured (the
// hand-off targets admit by suffix), and a "-N" split whenever the name
// already serves a different message — two invites named invite.ics
// never overwrite each other.
func attachSaveName(rows []attachRow, box string, seq int, orig, mt string) string {
	name := sanitizeAttachName(orig)
	ext := attachExtFor(mt)
	if ext != "" && !strings.HasSuffix(lowerASCII(name), ext) {
		name += ext
	}
	taken := func(n string) bool {
		for _, r := range rows {
			if r.saved == n && (r.box != box || r.seq != seq) {
				return true
			}
		}
		return false
	}
	if !taken(name) {
		return name
	}
	base, e := name, ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		base, e = name[:i], name[i:]
	}
	for n := 2; ; n++ {
		cand := base + "-" + itoa(int64(n)) + e
		if len(cand) <= attachNameMax && !taken(cand) {
			return cand
		}
	}
}

// saveAttach is readOne's attachment half: FETCH the part's section,
// decode its transfer encoding (base64 decodes — an .ics that stays
// b64 on disk is a saved lie GOCLOCK cannot merge — every other
// encoding writes the wire bytes verbatim), and publish under MAIL/
// with an index row. A failure prints attach-fail and returns — it is a
// named miss, never a read failure: the body already proved.
func saveAttach(c *imapClient, box string, seq int, a *partInfo) {
	name := a.attachName()
	data, err := c.section(seq, a.section())
	if err != nil {
		say("gomail: attach-fail " + name + " fetch")
		return
	}
	if a.encoding == "base64" {
		d, ok := b64decode(data)
		if !ok {
			say("gomail: attach-fail " + name + " base64")
			return
		}
		data = d
	}
	if len(data) > attachBytesMax {
		say("gomail: attach-fail " + name + " too-large")
		return
	}
	mt := a.mimeType()
	rows := attachIndex()
	saved := attachSaveName(rows, box, seq, name, mt)
	path := cachePath(saved)
	if err := cacheH.MkdirShare(cacheH.CacheDir()); err != nil {
		say("gomail: attach-fail " + name + " mkdir")
		return
	}
	if err := cacheH.WriteShare(path, data); err != nil {
		say("gomail: attach-fail " + name + " write")
		return
	}
	// Same (box, seq, orig) republishes in place; a re-read never
	// duplicates its own row.
	var kept []attachRow
	for _, r := range rows {
		if r.box == box && r.seq == seq && r.orig == name {
			continue
		}
		kept = append(kept, r)
	}
	attachIndexWrite(append(kept, attachRow{
		box: field(box), seq: seq, saved: saved, mt: field(mt), orig: field(name),
	}))
	say("gomail: saved " + path)
}

// saveAttachments walks one message's attachment leaves in order,
// bounded — the listing marker always prints (the part exists), the
// per-message bound names itself instead of swallowing the tail.
func saveAttachments(c *imapClient, box string, seq int, root *partInfo) {
	atts := attachments(root)
	for i, a := range atts {
		say("gomail: attach " + itoa(int64(seq)) + " " + a.attachName() + " " + a.mimeType())
		if i >= attachPerMsgMax {
			say("gomail: attach-fail " + a.attachName() + " bound")
			continue
		}
		saveAttach(c, box, seq, a)
	}
}

// --- the opens= route -------------------------------------------------

// attAppEntry is the manifest row as the route needs it — the same
// narrower read GOFILES' manifest.go makes of the APPS.TXT grammar.
type attAppEntry struct {
	bin   string
	args  []string
	opens []string
}

// attApps decodes /host/APPS.TXT; an unreadable share is an empty
// catalog — open then refuses by name rather than guessing at a
// handler (the seat's own rule).
func attApps() []attAppEntry {
	body, rc := attAppsRead(attachAppsPath, attachAppsMax)
	if rc < 0 {
		return nil
	}
	var out []attAppEntry
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) < 2 {
			continue
		}
		e := attAppEntry{bin: strings.TrimSpace(f[0])}
		if e.bin == "" {
			continue
		}
		v2 := false
		for _, tail := range f[4:] {
			k, val, ok := strings.Cut(strings.TrimSpace(tail), "=")
			if !ok {
				continue
			}
			switch k {
			case "v":
				v2 = val == "2"
			case "argv":
				e.args = strings.Fields(val)
				if len(e.args) > vi.ExecMaxArgs-1 {
					e.args = e.args[:vi.ExecMaxArgs-1]
				}
			case "opens":
				for _, tok := range strings.Split(val, ",") {
					if tok != "" {
						e.opens = append(e.opens, tok)
					}
				}
			}
		}
		if !v2 {
			e.args, e.opens = nil, nil
		}
		out = append(out, e)
	}
	return out
}

// opensAccepts is THE opens= admission rule — byte-for-byte the seat's
// (gotabwm/drag.go) and GOFILES' (files/manifest.go): a bare family
// word takes any subtype under it, a type/subtype spelling takes only
// itself, both exact-byte compares.
func opensAccepts(opens []string, mt string) bool {
	if mt == "" {
		return false
	}
	fam := mt
	if i := strings.IndexByte(mt, '/'); i >= 0 {
		fam = mt[:i]
	}
	for _, o := range opens {
		if o == mt {
			return true
		}
		if !strings.Contains(o, "/") && o == fam {
			return true
		}
	}
	return false
}

// attRoute resolves the declared mime spelling against the live
// manifest, first acceptor in manifest order — the dispatch the seat's
// open-with rows and GOFILES' submenu compute for the same spelling.
func attRoute(mt string) (attAppEntry, bool) {
	for _, e := range attApps() {
		if opensAccepts(e.opens, lowerASCII(mt)) {
			return e, true
		}
	}
	return attAppEntry{}, false
}

// openAttach is `open <name>` — and the window's 'o': resolve the index
// row, route its declared type through opens=, and exec the handler with
// [argv…, saved path]. A name the index never saved refuses as unknown;
// a type no manifest row accepts refuses by type — saved, never run.
// The exec's own refusal names itself too. Returns the marker tail the
// caller (the window's status line) may surface.
func openAttach(name string) error {
	r, ok := attachFind(name)
	if !ok {
		return protoErr("refuse open ", name)
	}
	e, ok := attRoute(r.mt)
	if !ok {
		return protoErr("refuse open ", name+" "+r.mt)
	}
	argv := append(append([]string{}, e.args...), cachePath(r.saved))
	pid, err := attExec(e.bin, argv...)
	if err != nil {
		return protoErr("refuse open ", name+" "+err.Error())
	}
	say("gomail: open " + name + " handler=" + e.bin + " pid=" + itoa(pid))
	return nil
}
