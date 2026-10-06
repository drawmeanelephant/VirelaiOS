package main

import "testing"

func TestArgvMode(t *testing.T) {
	for _, row := range []struct {
		args  []string
		child bool
	}{
		{nil, false},
		{[]string{"SVFIX.ELF"}, false},
		{[]string{"SVFIXCH.ELF", "child"}, true},
		{[]string{"SVFIXNV.ELF", "child"}, true},
		{[]string{"child"}, false},
	} {
		if got := childMode(row.args); got != row.child {
			t.Fatal(row.args, got)
		}
	}
}
