package main

import "testing"

func TestFixtureRows(t *testing.T) {
	for i, want := range []struct {
		level    byte
		tag, msg string
	}{{'D', "net", "line=000001"}, {'I', "ui", "line=000002"},
		{'W', "net", "line=000003"}, {'E', "ui", "line=000004"},
		{'D', "net", "line=000005"}} {
		level, tag, msg := fixtureRow(i + 1)
		if level != want.level || tag != want.tag || msg != want.msg {
			t.Fatalf("row %d: %c %s %s", i+1, level, tag, msg)
		}
	}
}
