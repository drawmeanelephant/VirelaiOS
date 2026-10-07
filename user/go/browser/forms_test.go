package main

import (
	"strings"
	"testing"

	"virelai/vi"
	"virelai/webrender"
)

func formApp(html, target string) *app {
	a := &app{hist: newHistory(), target: target, text: webrender.Bitmap{}}
	a.loadBody([]byte(html), target)
	return a
}

func TestSuccessfulGETControlsRemainInDOMOrder(t *testing.T) {
	a := formApp(`<form action="/search?old=1" method="get">
<input name="q" value="Zig é&"><input name="skip" value="no" disabled><input value="unnamed">
<input type="hidden" name="h" value="yes"><input type="checkbox" name="on" checked>
<input type="checkbox" name="off" value="skip"><select name="section"><option value="a">A</option><option value="b" selected>B</option></select>
<button name="go" value="ok">Search</button><button name="other" value="skip">Other</button></form>`, "https://example.com/doc")
	button := a.controls[len(a.controls)-2]
	target, kind := a.formTarget(button.form, button.node)
	if kind != "" || target != "https://example.com/search?q=Zig+%C3%A9%26&h=yes&on=on&section=b&go=ok" {
		t.Fatalf("target=%q error=%q", target, kind)
	}
}

func TestGETRefusesUnsupportedMethodsControlsAndCaps(t *testing.T) {
	for _, body := range []string{
		`<form method="post"><input name="q"></form>`,
		`<form><input type="password" name="p"></form>`,
		`<form><textarea name="t">no</textarea></form>`,
		`<form><select name="s" multiple><option>x</option></select></form>`,
		`<form><input name="` + strings.Repeat("x", 257) + `"></form>`,
	} {
		a := formApp(body, "https://example.com/")
		if len(a.controls) == 0 {
			t.Fatal("no test control")
		}
		if _, kind := a.formTarget(a.controls[0].form, nil); kind == "" {
			t.Fatalf("unsupported form accepted: %s", body)
		}
	}
}

func TestTranslatedEditingCancelsAndNormalizesURL(t *testing.T) {
	a := &app{hist: newHistory(), target: "/host/OLD.HTML"}
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: 0x0f, Flags: vi.ModCtrl})
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg1: 'é'})
	if a.urlEdit.text != "é" || a.quit {
		t.Fatalf("translated input=%q", a.urlEdit.text)
	}
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: keyEscape})
	if a.urlEditing || a.target != "/host/OLD.HTML" {
		t.Fatal("URL cancel changed navigation")
	}
	e := textEditor{}
	e.reset("", 4)
	e.key(0, 'é', 0)
	e.key(0, 'é', 0)
	e.key(0, 'x', 0)
	e.key(keyBacksp, 0, 0)
	if e.text != "é" {
		t.Fatalf("UTF-8 cap/backspace=%q", e.text)
	}
}

func TestControlTextCheckboxAndSelectAreInteractive(t *testing.T) {
	a := formApp(`<form><input name="q" value="old"><input name="c" type="checkbox">
<select name="s"><option value="a">A</option><option value="b">B</option></select></form>`, "http://example.com/")
	a.focusNextControl(false)
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg1: 'Z'})
	if a.controls[0].value != "Z" {
		t.Fatal("text control ignored translated editing")
	}
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: 0x2b})
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: 0x2c})
	if !a.controls[1].checked {
		t.Fatal("checkbox did not toggle")
	}
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: 0x2b})
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: keyDown})
	if a.controls[2].value != "b" {
		t.Fatal("select did not change")
	}
}

func TestStartSurfaceDoesNotDelayURLInputWithLoadSettleSleeps(t *testing.T) {
	prev := vi.SyscallHookForTest()
	defer vi.SetSyscallHookForTest(prev)
	sleeps := 0
	vi.SetSyscallHookForTest(func(slot, x0, x1, x2, x3 uintptr) int64 {
		if slot == vi.SlotSleep {
			sleeps++
		}
		return 0
	})
	a := &app{hist: newHistory(), text: webrender.Bitmap{}, tStart: vi.Nanos()}
	a.showStartSurface()
	a.settleIfNeeded()
	if !a.settled || sleeps != 0 {
		t.Fatalf("start surface blocks URL entry: settled=%v sleeps=%d", a.settled, sleeps)
	}
}

func TestSelectOptionsAreBoundedByDOMNotSilentlyDroppedAt64(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<form><select name="choice">`)
	for i := 0; i < 65; i++ {
		body.WriteString(`<option value="` + itoa(i) + `"`)
		if i == 64 {
			body.WriteString(` selected`)
		}
		body.WriteString(`>choice</option>`)
	}
	body.WriteString(`</select></form>`)
	a := &app{hist: newHistory(), text: webrender.Bitmap{}, target: "https://h/"}
	a.loadBody([]byte(body.String()), a.target)
	if len(a.controls) != 1 || a.controls[0].value != "64" {
		t.Fatal("a successful selected option was silently lost")
	}
}

func TestEditedControlPaintStaysInsideItsBoxWithoutLosingValue(t *testing.T) {
	a := formApp(`<form><input name="q" value="x"><button>Go</button></form>`, "https://h/")
	c := a.controls[0]
	c.value, c.changed = strings.Repeat("W", 256), true
	a.updateControlPaint()
	found := false
	for _, it := range a.lay.Items {
		if it.Kind != webrender.ItemText || it.Box == nil || it.Box.Node != c.node {
			continue
		}
		found = true
		st := webrender.Style{Size: it.Size, FontPx: it.FontPx, Mono: it.Mono}
		if a.lay.Text.Measure(it.Text, st) > c.rect.X+c.rect.W-4-it.X {
			t.Fatal("edited value paints outside its control")
		}
	}
	if !found || len(c.value) != 256 {
		t.Fatal("clipping changed the successful value or skipped its paint item")
	}
}

func TestInitialCheckedStateIsPaintedBeforeEditing(t *testing.T) {
	a := formApp(`<form><input type="checkbox" name="c" checked><button>Go</button></form>`, "https://h/")
	a.updateControlPaint()
	found := false
	for _, it := range a.lay.Items {
		if it.Kind == webrender.ItemText && it.Box != nil && it.Box.Node == a.controls[0].node {
			found = it.Text == "[x]"
		}
	}
	if !found {
		t.Fatal("initial checked state paints as unchecked")
	}
}
