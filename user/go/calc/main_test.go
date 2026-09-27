package main

import (
	"testing"

	"virelai/vi"
)

func TestCalcMarkerShapes(t *testing.T) {
	cases := []struct{ got, want string }{
		{markerOpen, "gocalc: open id="},
		{markerDeclare, "gocalc: declare accepted"},
		{markerPresent, "gocalc: present"},
		{markerSettingSubscribed, "gocalc: settings subscribed key=theme"},
		{markerSettingPresent, "gocalc: settings repaint key="},
		{markerResult, "gocalc: result "},
		{markerSaveErr, "gocalc: save error "},
		{markerClose, "gocalc: close"},
		{markerOK, "gocalc OK"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("marker = %q want %q", c.got, c.want)
		}
	}
}

func downASCII(ch uint32) vi.Event {
	return vi.Event{Kind: vi.EvKeyDown, Arg1: ch}
}

func downHID(usage uint32, ascii uint32) vi.Event {
	return vi.Event{Kind: vi.EvKeyDown, Arg0: usage, Arg1: ascii}
}

func TestKeyPathGateExpression(t *testing.T) {
	a := &app{}
	seq := []vi.Event{
		downASCII('1'),
		downASCII('2'),
		downASCII('-'),
		downASCII('3'),
		downASCII('='),
	}
	for _, ev := range seq {
		if !a.key(ev) {
			t.Fatalf("key %+v must be handled", ev)
		}
	}
	if a.eng.Display() != "9" {
		t.Fatalf("display = %q want 9", a.eng.Display())
	}
	if a.lastLine != "12-3=9" {
		t.Fatalf("lastLine = %q want 12-3=9", a.lastLine)
	}
}

func TestKeyPathAllFourOps(t *testing.T) {
	a := &app{}
	feed := func(keys string) {
		for i := 0; i < len(keys); i++ {
			if !a.key(downASCII(uint32(keys[i]))) {
				t.Fatalf("key %q must be handled", keys[i:i+1])
			}
		}
	}
	feed("6*7=")
	if a.eng.Display() != "42" {
		t.Fatalf("6*7 = %q want 42", a.eng.Display())
	}
	a.doClear()
	feed("8/2=")
	if a.eng.Display() != "4" {
		t.Fatalf("8/2 = %q want 4", a.eng.Display())
	}
	a.doClear()
	feed("12+34=")
	if a.eng.Display() != "46" {
		t.Fatalf("12+34 = %q want 46", a.eng.Display())
	}
}

func TestEnterAndEscapeHID(t *testing.T) {
	a := &app{}
	a.key(downASCII('9'))
	a.key(downASCII('+'))
	a.key(downASCII('1'))
	if !a.key(downHID(keyEnter, '\n')) {
		t.Fatal("Enter HID usage must evaluate")
	}
	if a.eng.Display() != "10" {
		t.Fatalf("9+1 via Enter = %q want 10", a.eng.Display())
	}
	if !a.key(downHID(keyEscape, 0)) {
		t.Fatal("Escape HID usage must clear")
	}
	if a.eng.Display() != "0" || a.expr != "" {
		t.Fatal("Escape must clear")
	}
}

func TestClearKeyAndPad(t *testing.T) {
	a := &app{}
	a.key(downASCII('7'))
	a.key(downASCII('c'))
	if a.eng.Display() != "0" || a.expr != "" {
		t.Fatal("c must clear")
	}
	if !a.press("8") || !a.press("+") || !a.press("2") || !a.press("=") {
		t.Fatal("pad presses must be handled")
	}
	if a.eng.Display() != "10" {
		t.Fatalf("pad 8+2 = %q want 10", a.eng.Display())
	}
	if !a.press("C") {
		t.Fatal("pad C must clear")
	}
	if a.eng.Display() != "0" {
		t.Fatal("pad C must reset the display")
	}
}

func TestCtrlIsIgnored(t *testing.T) {
	a := &app{}
	ev := vi.Event{Kind: vi.EvKeyDown, Flags: vi.ModCtrl, Arg1: '1'}
	if a.key(ev) {
		t.Fatal("ctrl chords must not type digits")
	}
}
