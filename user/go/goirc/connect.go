package main

// connect.go — issue #3779: the first-run connect face. A bare launch (the
// launcher row or the seat's re-exec — argv carries nothing) with no
// staged session blob has no host/nick to dial; the window now opens a
// connect face instead of the old usage+exit dead end:
//
//   - no saved server → sequential appkit prompts: server (host[:port]),
//     then nick. The answers fill cfg exactly like argv would have.
//   - a saved server  → the appkit list-select idiom: the /host/GOIRC.TXT
//     row plus a trailing "New server…" row that falls into the prompts.
//
// The first registration after a face launch persists the resolved config
// back to /host/GOIRC.TXT (the GOMAIL.TXT codec discipline), so the next
// bare launch is a picker, not a prompt — and a stored plain=on consent
// answer never re-asks. Esc on any face step prints the same usage exit
// the empty launch used to reach.
//
// Markers (the goirc: prefix, the gate's grep discipline):
//
//	goirc: connect ask          the face is on the scanout
//	goirc: connect saved        the picker chose the stored row
//	goirc: connect cancel       Esc — the usage exit follows
//	goirc: config <path>        a saved server was read
//	goirc: config saved         GOIRC.TXT published
//	goirc: config write-fail    the publish failed
//
// Argv paths are untouched: the face arms only when runUI inherits a
// config with no host or no nick — a shape argv launches never reach
// (parseArgs refuses them before runUI).

import (
	"errors"
	"strconv"
	"strings"

	"virelai/appkit"
	uidraw "virelai/draw"
	"virelai/theme"
	"virelai/vi"
	"virelai/vsys"
	"virelai/widgets"
)

// connectConfigPath is the app's saved-server file — the GOMAIL.TXT
// precedent (a key=value app file on the share, not the kernel table).
const connectConfigPath = "/host/GOIRC.TXT"

const (
	connectCfgMaxBytes = 4096
	connectCfgMaxRows  = 16
	connectCfgMaxKey   = 8
	connectCfgMaxVal   = 255

	connectPlateW = 300
	connectRowH   = 24
)

// dlgFor tags which modal the shared u.dlg serves — the M147b plaintext
// consent ask or a connect-face prompt.
const (
	dlgConsent = iota
	dlgServer
	dlgNick
)

// cfgOnOff is the file codec's boolean spelling.
func cfgOnOff(v string) (bool, bool) {
	switch v {
	case "on", "yes", "true", "1":
		return true, true
	case "off", "no", "false", "0":
		return false, true
	}
	return false, false
}

// applyConnectConfig decodes the file body — strict, the account.go
// discipline: an unknown key, a malformed row or a missing host/nick
// refuses the file whole rather than half-filling the face.
func applyConnectConfig(body []byte) (Config, bool) {
	cfg := Config{Port: 6667, Reconnect: true}
	rows := 0
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		rows++
		if rows > connectCfgMaxRows {
			return Config{}, false
		}
		k := strings.IndexByte(line, '=')
		if k <= 0 || k > connectCfgMaxKey {
			return Config{}, false
		}
		key, val := strings.TrimSpace(line[:k]), strings.TrimSpace(line[k+1:])
		if key == "" || len(val) > connectCfgMaxVal {
			return Config{}, false
		}
		switch key {
		case "host":
			cfg.Host = val
		case "port":
			n, ok := parseUint(val)
			if !ok || n <= 0 || n > 65535 {
				return Config{}, false
			}
			cfg.Port = uint16(n)
		case "nick":
			cfg.Nick = val
		case "tls":
			on, ok := cfgOnOff(val)
			if !ok {
				return Config{}, false
			}
			cfg.TLS = on
		case "sni":
			cfg.ServerName = val
		case "plain":
			on, ok := cfgOnOff(val)
			if !ok {
				return Config{}, false
			}
			cfg.AllowPlain = on
		default:
			return Config{}, false
		}
	}
	// A saved server is a complete connect target or nothing.
	return cfg, cfg.Host != "" && cfg.Nick != ""
}

// connectBody serializes the saved server — the strict codec's inverse.
func (c Config) connectBody() []byte {
	on := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	return []byte("#v1\n" +
		"host=" + c.Host + "\n" +
		"port=" + vsys.Itoa64(int64(c.Port)) + "\n" +
		"nick=" + c.Nick + "\n" +
		"tls=" + on(c.TLS) + "\n" +
		"sni=" + c.ServerName + "\n" +
		"plain=" + on(c.AllowPlain) + "\n")
}

// loadConnectConfig reads the saved server; a missing or refused file is
// simply absent — the first-run prompts are the fallback, not an error.
func loadConnectConfig(path string) (Config, bool) {
	body, rc := vi.ReadFileAll(path, connectCfgMaxBytes)
	if rc < 0 {
		return Config{}, false
	}
	return applyConnectConfig(body)
}

// writeConnectConfig publishes the resolved config — WriteFileSafe, the
// whole-file rewrite every share write here uses.
func writeConnectConfig(path string, cfg Config) error {
	if rc := vi.WriteFileSafe(path, cfg.connectBody()); rc < 0 {
		return errors.New("publish " + strconv.FormatInt(rc, 10))
	}
	return nil
}

// parseServerArg reads the prompt's server field: "host" or "host:port".
// A colon whose tail is not a 1..65535 decimal refuses the whole field —
// '10.0.0.2:abc' is a typo to re-ask, not a host named '10.0.0.2:abc'.
// port 0 keeps cfg's default.
func parseServerArg(s string) (host string, port uint16, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		h, tail := s[:i], s[i+1:]
		n, ok := parseUint(tail)
		if !ok || n <= 0 || n > 65535 || h == "" {
			return "", 0, false
		}
		return h, uint16(n), true
	}
	if strings.IndexAny(s, " \t") >= 0 {
		return "", 0, false
	}
	return s, 0, true
}

// nickByte is the RFC 1459 nick alphabet — mentionMatch's word-edge set.
func nickByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '-' || c == '_' ||
		c == '[' || c == ']' || c == '{' || c == '}' ||
		c == '\\' || c == '|' || c == '`' || c == '^'
}

// parseNickArg reads the nick field: the first whitespace-delimited token
// in the nick alphabet. Anything else re-asks.
func parseNickArg(s string) (string, bool) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return "", false
	}
	n := f[0]
	for i := 0; i < len(n); i++ {
		if !nickByte(n[i]) {
			return "", false
		}
	}
	return n, true
}

// --- the saved-server picker ------------------------------------------------

// serverPick is the connect face's saved-server select — the appkit
// ListController idiom on a modal plate. Row 0 is the stored server; the
// trailing "New server…" row falls into the prompt chain.
type serverPick struct {
	ctl   *appkit.ListController
	r     widgets.Rect // the modal plate
	saved Config
}

func newServerPick(saved Config, w, h int) *serverPick {
	tok := theme.Current
	p := &serverPick{saved: saved}
	pw, ph := connectPlateW, tok.PadSM+20+tok.PadSM+2*connectRowH+tok.PadMD
	p.r = widgets.Rect{X: (w - pw) / 2, Y: (h - ph) / 2, W: pw, H: ph}
	p.ctl = appkit.NewListController(&widgets.List{
		R: widgets.Rect{
			X: p.r.X + tok.PadMD,
			Y: p.r.Y + tok.PadSM + 20 + tok.PadSM,
			W: pw - 2*tok.PadMD,
			H: 2 * connectRowH,
		},
		Items: []string{
			saved.Host + ":" + vsys.Itoa64(int64(saved.Port)) + " as " + saved.Nick,
			"New server…",
		},
		RowH:  connectRowH,
		Sel:   0,
		Fg:    tok.Ink,
		Bg:    tok.ChromeBg,
		SelBg: tok.Selection,
	})
	p.ctl.SetFocused(true)
	return p
}

func (p *serverPick) draw(cv widgets.Canvas) {
	tok := theme.Current
	cv.FillRoundedRect(p.r, 8, uidraw.Opaque(tok.Surface))
	cv.StrokeRoundedRect(p.r, 8, tok.BorderW, uidraw.Opaque(tok.Border))
	title := widgets.Rect{
		X: p.r.X + tok.PadMD, Y: p.r.Y + tok.PadSM,
		W: p.r.W - 2*tok.PadMD, H: 20,
	}
	widgets.DrawText(cv, title, "Connect to a server", uidraw.Opaque(tok.Text))
	p.ctl.Draw(cv)
}

// --- the face's uiApp halves -------------------------------------------------

// openConnect arms the connect face: the saved server's picker when
// GOIRC.TXT parses, the server prompt otherwise.
func (u *uiApp) openConnect() {
	if saved, ok := loadConnectConfig(connectConfigPath); ok {
		vi.ConsoleLine("goirc: config " + connectConfigPath)
		u.pick = newServerPick(saved, int(u.ta.W), int(u.ta.H))
		return
	}
	u.openServerPrompt()
}

// openPrompt raises a bounded single-line dialog for the connect face.
func (u *uiApp) openPrompt(title, message string, forWhat int) {
	d := appkit.NewPrompt(title, message)
	d.R = widgets.Rect{
		X: (int(u.ta.W) - d.R.W) / 2,
		Y: (int(u.ta.H) - d.R.H) / 2,
		W: d.R.W, H: d.R.H,
	}
	u.dlg, u.dlgFor = &d, forWhat
}

func (u *uiApp) openServerPrompt() {
	u.openPrompt("IRC server", "host or host:port", dlgServer)
}

func (u *uiApp) openNickPrompt() {
	u.openPrompt("IRC nickname", "the nick to register as", dlgNick)
}

// dialogDone consumes the closed dialog's result by role.
func (u *uiApp) dialogDone() {
	switch u.dlgFor {
	case dlgServer:
		u.serverAnswer()
	case dlgNick:
		u.nickAnswer()
	default:
		u.consentAnswer()
	}
}

// serverAnswer applies the server prompt's result; a malformed field
// re-asks rather than half-filling the config.
func (u *uiApp) serverAnswer() {
	res := u.dlg.Result
	u.dlg = nil
	if res == "cancel" {
		u.cancelConnect()
		return
	}
	host, port, ok := parseServerArg(res)
	if !ok {
		u.status("usage: host[:port]")
		u.openServerPrompt()
		return
	}
	u.cfg.Host = host
	if port != 0 {
		u.cfg.Port = port
	}
	u.openNickPrompt()
}

// nickAnswer applies the nick prompt's result — the last face step before
// the session's normal consent-or-dial path takes over.
func (u *uiApp) nickAnswer() {
	res := u.dlg.Result
	u.dlg = nil
	if res == "cancel" {
		u.cancelConnect()
		return
	}
	nick, ok := parseNickArg(res)
	if !ok {
		u.status("usage: nick")
		u.openNickPrompt()
		return
	}
	u.cfg.Nick = nick
	u.fromFace = true
	u.beginSession()
}

// pickChoose applies the picker's committed row: the stored server fills
// cfg (consent included — the stored answer never re-asks) or the
// "New server…" row opens the prompt chain.
func (u *uiApp) pickChoose() {
	sel := u.pick.ctl.List.Sel
	saved := u.pick.saved
	u.pick = nil
	if sel != 0 {
		u.openServerPrompt()
		return
	}
	u.cfg.Host = saved.Host
	u.cfg.Port = saved.Port
	u.cfg.Nick = saved.Nick
	u.cfg.TLS = saved.TLS
	u.cfg.ServerName = saved.ServerName
	u.cfg.AllowPlain = saved.AllowPlain
	vi.ConsoleLine("goirc: connect saved")
	u.fromFace = true
	u.beginSession()
}

// beginSession hands a completed face config to the same gate argv-driven
// launches take: the consent ask when plaintext is unarmed, else the
// client starts.
func (u *uiApp) beginSession() {
	if u.needConsent() {
		u.openConsent()
		u.draw()
		u.ta.Present()
		vi.ConsoleLine("goirc: consent ask")
		return
	}
	u.startClient()
}

// cancelConnect is the face's Esc — the same usage + exit the empty
// launch printed before the face existed.
func (u *uiApp) cancelConnect() {
	vi.ConsoleLine("goirc: connect cancel")
	usage()
	u.ta.CloseAndExit(exitUsage)
}

// saveConnectConfig publishes GOIRC.TXT — the face's persist half, called
// on the first registration so the file holds a connect target that
// provably dialed (and the consent bit it earned).
func (u *uiApp) saveConnectConfig() {
	if err := writeConnectConfig(connectConfigPath, u.cfg); err != nil {
		vi.ConsoleLine("goirc: config write-fail " + err.Error())
		return
	}
	vi.ConsoleLine("goirc: config saved")
}
