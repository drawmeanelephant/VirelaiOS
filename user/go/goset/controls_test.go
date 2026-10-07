package main

import (
	"testing"
	"unicode/utf8"

	"virelai/appkit"
	"virelai/vi"
	"virelai/widgets"
)

func TestBoundedFieldMatchesExistingPlainEditor(t *testing.T) {
	original := appkit.NewTextField(widgets.Rect{}, 8)
	field := textField{Max: 8}
	for _, ev := range []vi.Event{
		{Kind: vi.EvKeyDown, Arg1: 'a'}, {Kind: vi.EvKeyDown, Arg1: 'é'},
		{Kind: vi.EvKeyDown, Arg1: '界'}, {Kind: vi.EvKeyDown, Arg0: keyLeft},
		{Kind: vi.EvKeyDown, Arg1: 'x'}, {Kind: vi.EvKeyDown, Arg0: keyBackspace},
		{Kind: vi.EvKeyDown, Arg0: keyDelete}, {Kind: vi.EvKeyDown, Arg0: keyEnd},
		{Kind: vi.EvKeyDown, Arg1: '😀'}, {Kind: vi.EvKeyDown, Arg0: keyHome},
		{Kind: vi.EvKeyDown, Arg1: 'Q', Flags: vi.ModCtrl},
		{Kind: vi.EvKeyDown, Arg1: 'Q', Flags: vi.ModAlt},
	} {
		key, _ := normalizeKey(ev)
		oldKey, _ := appkit.NormalizeKey(ev)
		field.OnKey(key)
		original.OnKey(oldKey)
		if field.Value() != original.Value() || field.CaretPosition() != original.CaretPosition() ||
			!utf8.ValidString(field.Value()) {
			t.Fatal(ev, field.Value(), field.CaretPosition(), original.Value(), original.CaretPosition())
		}
	}
	field.SetValue("1234567界")
	original.SetValue("1234567界")
	if field.Value() != original.Value() || field.Value() != "1234567" {
		t.Fatal("SetValue split a scalar", field.Value())
	}
	field.Clear()
	if field.Value() != "" || field.CaretPosition() != 0 {
		t.Fatal("clear")
	}
}

func TestBoundedControlsKeepFocusAndScrollContract(t *testing.T) {
	a := newPanel(nil)
	a.list = widgets.List{R: widgets.Rect{W: 20, H: 20}, RowH: 10,
		Items: []string{"one", "two", "three"}}
	a.focus.Focus(0)
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: keyEnd})
	if a.sel != 2 || a.list.ScrollTop != 1 {
		t.Fatal(a.sel, a.list.ScrollTop)
	}
	a.focus.Focus(1)
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: keyTab, Flags: vi.ModShift})
	if _, i := a.focus.Current(); i != 0 || !a.list.Focused || a.input.Focused {
		t.Fatal("shift-tab focus")
	}
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: keyTab, Flags: vi.ModShift})
	if _, i := a.focus.Current(); i != 3 || !a.quitBtn.Focused {
		t.Fatal("focus wrap")
	}
}
