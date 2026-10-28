package main

// conv_v3_test.go — the M205b (#3809) pins: the act records (@@call,
// @@consent, @@result), the strict ordering and grammar refusals, the
// write-on-demand version pick, and the v1/v2 goldens still decoding.
// Kept in its own file so card c's tests can work in parallel.

import (
	"strings"
	"testing"
)

func TestConvCodecV3RoundTrip(t *testing.T) {
	c := &conversation{name: "C0007", title: "Acts", turns: []turn{
		{role: roleSystem, text: "You are terse."},
		{role: roleUser, text: "read the readme"},
		{role: roleCall, arg: "fs.read", text: "path=/host/README"},
		{role: roleConsent, arg: "allow"},
		{role: roleResult, arg: "ok", text: "line one\n@@looks-like-a-marker"},
		{role: roleModel, text: "the readme says line one."},
		{role: roleCall, arg: "gosh.run", text: "cmd=rm -rf /"},
		{role: roleConsent, arg: "deny"},
		{role: roleResult, arg: "refuse:denied"},
	}}
	b := encodeConv(c)
	if !strings.HasPrefix(string(b),
		convMagicV3+"\n@@title Acts\n@@system\n") {
		t.Fatalf("v3 head wrong:\n%s", b)
	}
	// The marker parameters ride their own lines; a @@-looking body line
	// stays space-protected inside the result body.
	for _, want := range []string{
		"@@call fs.read\npath=/host/README\n",
		"@@consent allow\n",
		"@@result ok\nline one\n @@looks-like-a-marker\n",
		"@@call gosh.run\n",
		"@@consent deny\n",
		"@@result refuse:denied\n",
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("wire missing %q:\n%s", want, b)
		}
	}
	d, ok := decodeConv(b)
	if !ok {
		t.Fatal("v3 round trip refused")
	}
	if d.title != "Acts" || len(d.turns) != len(c.turns) {
		t.Fatalf("decoded %+v", d)
	}
	for i, w := range c.turns {
		if d.turns[i] != w {
			t.Fatalf("turn %d = %+v want %+v", i, d.turns[i], w)
		}
	}
	// Re-encoding the decode is byte-exact — the file round-trips whole.
	if got := string(encodeConv(d)); got != string(b) {
		t.Fatalf("re-encode drifted:\n%s", got)
	}
}

// v3 is write-on-demand like v2: no act records means the v1/v2 bytes
// are untouched.
func TestConvCodecV3OnlyWhenActs(t *testing.T) {
	plain := &conversation{turns: []turn{{role: roleUser, text: "hi"}}}
	if !strings.HasPrefix(string(encodeConv(plain)), convMagicV1+"\n") {
		t.Fatal("act-less conv must stay v1")
	}
	titled := &conversation{title: "T",
		turns: []turn{{role: roleUser, text: "hi"}}}
	if !strings.HasPrefix(string(encodeConv(titled)), convMagicV2+"\n") {
		t.Fatal("titled act-less conv must stay v2")
	}
	// One act record promotes the whole file, title or not.
	acted := &conversation{turns: []turn{{role: roleCall, arg: "proc.list"}}}
	if b := encodeConv(acted); !strings.HasPrefix(string(b),
		convMagicV3+"\n@@call proc.list\n") {
		t.Fatalf("an act must promote to v3:\n%s", b)
	}
}

// A call the user never answered — or never saw a result for — is a
// legal file: the act closes where the next record starts. Only the
// explicit mis-orderings refuse.
func TestConvCodecV3OpenActs(t *testing.T) {
	for name, body := range map[string]string{
		"call pending":   "GOASSIST-CHAT v3\n@@call proc.list\n@@user\nnever mind\n",
		"call at eof":    "GOASSIST-CHAT v3\n@@user\nps\n@@call proc.list\n",
		"call then call": "GOASSIST-CHAT v3\n@@call fs.read\na=1\n@@call fs.write\nb=2\n",
		"no consent":     "GOASSIST-CHAT v3\n@@call proc.list\n@@result ok\nPID 1\n",
	} {
		if _, ok := decodeConv([]byte(body)); !ok {
			t.Fatalf("%s: refused %q", name, body)
		}
	}
}

func TestConvCodecV3Refuses(t *testing.T) {
	cases := map[string]string{
		// Act records are v3-only; v1/v2 files keep their old grammar.
		"call in v1":    "GOASSIST-CHAT v1\n@@call fs.read\nx\n",
		"call in v2":    "GOASSIST-CHAT v2\n@@call fs.read\nx\n",
		"consent in v2": "GOASSIST-CHAT v2\n@@consent allow\n",
		"result in v2":  "GOASSIST-CHAT v2\n@@result ok\n",
		// Ordering: consent and result need an open @@call.
		"result alone":         "GOASSIST-CHAT v3\n@@result ok\n",
		"consent alone":        "GOASSIST-CHAT v3\n@@consent allow\n",
		"result after user":    "GOASSIST-CHAT v3\n@@call fs.read\n@@user\nhi\n@@result ok\n",
		"result after result":  "GOASSIST-CHAT v3\n@@call fs.read\n@@result ok\n@@result ok\n",
		"consent after result": "GOASSIST-CHAT v3\n@@call fs.read\n@@result ok\n@@consent allow\n",
		"consent twice":        "GOASSIST-CHAT v3\n@@call fs.read\n@@consent allow\n@@consent deny\n",
		// Marker parameter grammars.
		"call no verb":        "GOASSIST-CHAT v3\n@@call\nx\n",
		"call upper verb":     "GOASSIST-CHAT v3\n@@call FS.READ\nx\n",
		"call verb plus args": "GOASSIST-CHAT v3\n@@call fs.read extra\nx\n",
		"verb over bound": "GOASSIST-CHAT v3\n@@call " +
			strings.Repeat("v", convVerbMax+1) + "\nx\n",
		"consent bad answer":  "GOASSIST-CHAT v3\n@@call fs.read\n@@consent maybe\n",
		"consent extra words": "GOASSIST-CHAT v3\n@@call fs.read\n@@consent allow now\n",
		"consent with body":   "GOASSIST-CHAT v3\n@@call fs.read\n@@consent allow\nnote\n",
		"result bad state":    "GOASSIST-CHAT v3\n@@call fs.read\n@@result fine\n",
		"result empty reason": "GOASSIST-CHAT v3\n@@call fs.read\n@@result refuse:\n",
		"result reason space": "GOASSIST-CHAT v3\n@@call fs.read\n@@result refuse:not allowed\n",
		"reason over bound": "GOASSIST-CHAT v3\n@@call fs.read\n@@result refuse:" +
			strings.Repeat("r", convReasonMax+1) + "\n",
		// Chat records still take no parameter.
		"user arg":   "GOASSIST-CHAT v3\n@@user hi\n",
		"system arg": "GOASSIST-CHAT v3\n@@system x\nbe terse\n",
		// The rest of the strict grammar is unchanged.
		"system not first": "GOASSIST-CHAT v3\n@@user\nhi\n@@system\nx\n",
		"unknown record":   "GOASSIST-CHAT v3\n@@bogus\nx\n",
		// Act body bounds.
		"args over bound": "GOASSIST-CHAT v3\n@@call fs.read\n" +
			strings.Repeat("a", convArgsMax+1) + "\n",
		"out over bound": "GOASSIST-CHAT v3\n@@call fs.read\n@@result ok\n" +
			strings.Repeat("o", convOutMax+1) + "\n",
	}
	for name, body := range cases {
		if _, ok := decodeConv([]byte(body)); ok {
			t.Fatalf("%s: accepted %q", name, body)
		}
	}
}

// The result bound: under convOutMax the body stores verbatim; past it
// the writer's cut ends on the named mark, and only that cut decodes.
func TestConvCodecV3Truncation(t *testing.T) {
	if got := cutResultBody("small"); got != "small" {
		t.Fatalf("under-bound output must pass through: %q", got)
	}
	out := strings.Repeat("o", convOutMax) + "BEYOND-THE-BOUND"
	cut := cutResultBody(out)
	if !strings.HasPrefix(cut, out[:convOutMax]) ||
		!strings.HasSuffix(cut, "\n"+convTruncMark) ||
		len(cut) != convOutMax+1+len(convTruncMark) {
		t.Fatalf("cut body = %d bytes", len(cut))
	}
	c := &conversation{turns: []turn{
		{role: roleCall, arg: "ui.screenshot"},
		{role: roleConsent, arg: "allow"},
		{role: roleResult, arg: "ok", text: cut},
	}}
	d, ok := decodeConv(encodeConv(c))
	if !ok || d.turns[2].text != cut {
		t.Fatalf("a cut body must round-trip: ok=%v", ok)
	}
	// Over the bound WITHOUT the mark refuses (the "out over bound" case
	// above); exactly at the bound needs no mark.
	exact := &conversation{turns: []turn{
		{role: roleCall, arg: "proc.list"},
		{role: roleResult, arg: "ok", text: strings.Repeat("x", convOutMax)},
	}}
	if _, ok := decodeConv(encodeConv(exact)); !ok {
		t.Fatal("an at-bound body must not need the mark")
	}
}

// The goldens the card names: v1 and v2 files already on disk decode
// unchanged under the v3 codec.
func TestConvCodecV1V2Goldens(t *testing.T) {
	v1 := "GOASSIST-CHAT v1\n@@user\nhi there\n@@assistant\nthe answer\n"
	d, ok := decodeConv([]byte(v1))
	if !ok || len(d.turns) != 2 || d.turns[0].role != roleUser ||
		d.turns[0].text != "hi there" || d.turns[1].text != "the answer" {
		t.Fatalf("v1 golden: ok=%v %+v", ok, d)
	}
	if d.title != "" {
		t.Fatalf("v1 must read title-less: %q", d.title)
	}
	v2 := "GOASSIST-CHAT v2\n@@title Trip plan\n@@system\nbe terse\n" +
		"@@user\nlisbon\n"
	d, ok = decodeConv([]byte(v2))
	if !ok || d.title != "Trip plan" || len(d.turns) != 2 ||
		d.turns[0].role != roleSystem || d.turns[1].text != "lisbon" {
		t.Fatalf("v2 golden: ok=%v %+v", ok, d)
	}
}

// A titled v3 conversation still lists its title — the sidebar's cheap
// header read knows the new magic.
func TestConvTitleOfV3(t *testing.T) {
	b := encodeConv(&conversation{title: "Acts", turns: []turn{
		{role: roleCall, arg: "proc.list"},
	}})
	if got := convTitleOf(b); got != "Acts" {
		t.Fatalf("v3 title = %q", got)
	}
	if got := convTitleOf([]byte("GOASSIST-CHAT v3\n@@call proc.list\n")); got != "" {
		t.Fatalf("untitled v3 must read empty: %q", got)
	}
}

// Act records render as their own muted lines, distinct from the chat
// turns: call/consent/result headers, args and output in the code face.
func TestRenderActs(t *testing.T) {
	c := &conversation{turns: []turn{
		{role: roleUser, text: "read it"},
		{role: roleCall, arg: "fs.read", text: "path=/host/README"},
		{role: roleConsent, arg: "deny"},
		{role: roleResult, arg: "refuse:denied"},
	}}
	lines := renderConv(c, 80*8, cellMeasure)
	var heads, bodies int
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l.text, "call> fs.read"):
			heads++
			if l.kind != rlMeta {
				t.Fatalf("call head kind = %d: %+v", l.kind, l)
			}
		case strings.HasPrefix(l.text, "consent> deny"),
			strings.HasPrefix(l.text, "result> refuse:denied"):
			heads++
			if l.kind != rlMeta {
				t.Fatalf("act head kind = %d: %+v", l.kind, l)
			}
		case l.text == "path=/host/README":
			bodies++
			if l.kind != rlCode {
				t.Fatalf("args body kind = %d: %+v", l.kind, l)
			}
		}
	}
	if heads != 3 || bodies != 1 {
		t.Fatalf("render heads=%d bodies=%d: %+v", heads, bodies, lines)
	}
	if !strings.HasPrefix(lines[0].text, "you>") {
		t.Fatalf("the user turn must still render as chat: %+v", lines[0])
	}
}
