package main

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"virelai/vi"
)

func TestFilters(t *testing.T) {
	tests := []struct {
		name string
		f    filter
		want []string
	}{
		{"all", filter{level: 'D'}, []string{"A 1 D net debug", "A 2 I ui info", "A 3 W net warn", "A 4 E ui error", "A plain"}},
		{"level", filter{level: 'W'}, []string{"A 3 W net warn", "A 4 E ui error"}},
		{"tag", filter{level: 'D', tag: "net"}, []string{"A 1 D net debug", "A 3 W net warn"}},
		{"grep", filter{level: 'D', grep: "warn"}, []string{"A 3 W net warn"}},
		{"app", filter{level: 'D', apps: []string{"B"}}, nil},
		{"combined", filter{level: 'W', tag: "net", grep: "warn", apps: []string{"A"}}, []string{"A 3 W net warn"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newViewer(tt.f)
			added := v.observe("A", []byte("1 D net debug\n2 I ui info\n3 W net warn\n4 E ui error\nplain\n"))
			if got := texts(added); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rows=%q want=%q", got, tt.want)
			}
			if !reflect.DeepEqual(texts(v.visible()), tt.want) {
				t.Fatalf("visible=%q", texts(v.visible()))
			}
		})
	}
}

func TestArrivalInterleavingAndNoReplay(t *testing.T) {
	v := newViewer(filter{level: 'D'})
	v.observe("B", []byte("1 I net b1\n"))
	v.observe("A", []byte("1 D ui a1\n"))
	v.observe("B", []byte("1 I net b1\n2 E ui b2\n"))
	want := []string{"B 1 I net b1", "A 1 D ui a1", "B 2 E ui b2"}
	if got := texts(v.visible()); !reflect.DeepEqual(got, want) {
		t.Fatalf("arrival=%q", got)
	}
	if got := v.observe("B", []byte("1 I net b1\n2 E ui b2\n")); len(got) != 0 {
		t.Fatalf("replayed=%+v", got)
	}
}

func TestContentFollowFindsSameSizeGap(t *testing.T) {
	v := newViewer(filter{level: 'E'}) // gaps survive even a filter hiding all rows
	first := sequenceRing(100001, 100032)
	next := sequenceRing(100065, 100096)
	if len(first) != len(next) {
		t.Fatal("fixture must have equal byte sizes")
	}
	v.observe("A", first)
	added := v.observe("A", next)
	if len(added) != 1 || added[0].text() != "gap app=A lost=32" {
		t.Fatalf("same-size gap=%+v", added)
	}
}

func TestInitialGapAndPlainContentOverlap(t *testing.T) {
	v := newViewer(filter{level: 'D'})
	got := texts(v.observe("A", []byte("4 W net retained\n")))
	if !reflect.DeepEqual(got, []string{"gap app=A lost=3", "A 4 W net retained"}) {
		t.Fatalf("initial=%q", got)
	}
	v.observe("P", []byte("one\ntwo\n"))
	if got := texts(v.observe("P", []byte("two\nsix\n"))); !reflect.DeepEqual(got, []string{"P six"}) {
		t.Fatalf("plain same-size=%q", got)
	}
	// No sequence means no invented count for overwritten plain rows.
	if got := texts(v.observe("P", []byte("new\n"))); !reflect.DeepEqual(got, []string{"P new"}) {
		t.Fatalf("plain replacement=%q", got)
	}
}

func TestFilteringIsNotLoss(t *testing.T) {
	v := newViewer(filter{level: 'E'})
	if got := v.observe("A", []byte("1 D net hidden\n2 I ui hidden\n")); len(got) != 0 {
		t.Fatalf("filtered=%+v", got)
	}
	got := texts(v.observe("A", []byte("2 I ui hidden\n3 W net hidden\n4 E ui visible\n")))
	if !reflect.DeepEqual(got, []string{"A 4 E ui visible"}) {
		t.Fatalf("filters invented a gap: %q", got)
	}
}

func TestFilteredExportAndNumbering(t *testing.T) {
	v := newViewer(filter{level: 'W', tag: "net"})
	v.observe("B", []byte("1 W net b\n2 E ui hidden\n"))
	v.observe("A", []byte("1 D net hidden\n2 W net a\n"))
	files := map[string][]byte{exportDir + "/A-1.TXT": []byte("old")}
	paths, rc := v.exportView(func(path string) (bool, int64) {
		_, ok := files[path]
		return ok, 0
	}, func(path string, body []byte) int64 {
		files[path] = append([]byte(nil), body...)
		return 0
	})
	wantPaths := []string{exportDir + "/A-2.TXT", exportDir + "/B-1.TXT"}
	if rc != 0 || !reflect.DeepEqual(paths, wantPaths) ||
		string(files[wantPaths[0]]) != "A 2 W net a\n" ||
		string(files[wantPaths[1]]) != "B 1 W net b\n" ||
		string(files[exportDir+"/A-1.TXT"]) != "old" {
		t.Fatalf("paths=%v rc=%d files=%q", paths, rc, files)
	}
	_, rc = v.exportView(func(string) (bool, int64) { return false, -7 },
		func(string, []byte) int64 { t.Fatal("write after failed lookup"); return 0 })
	if rc != -7 {
		t.Fatalf("lookup refusal=%d", rc)
	}
	_, rc = v.exportView(func(string) (bool, int64) { return false, 0 },
		func(string, []byte) int64 { return -8 })
	if rc != -8 {
		t.Fatalf("write refusal=%d", rc)
	}
}

func TestViewerBounds(t *testing.T) {
	v := newViewer(filter{level: 'D'})
	for i := 1; i < viewMaxRows*2; i++ {
		v.observe("A", []byte(strconv.Itoa(i)+" I net row\n"))
	}
	if len(v.rows) != viewMaxRows {
		t.Fatalf("retained=%d", len(v.rows))
	}
	for i := 0; i < vi.MaxDirEntries*2; i++ {
		v.observe("APP"+strconv.Itoa(i), []byte("1 I net row\n"))
	}
	if len(v.cursors) != vi.MaxDirEntries {
		t.Fatalf("apps=%d", len(v.cursors))
	}
}

func TestOptions(t *testing.T) {
	o, ok := parseOptions([]string{"-text", "-app", "B,A", "-level", "W", "-tag", "net", "-grep", "hello", "-polls", "8", "-follow", "export"})
	if !ok || !o.text || !o.follow || !o.export || o.polls != 8 ||
		!reflect.DeepEqual(o.filter.apps, []string{"A", "B"}) ||
		o.filter.level != 'W' || o.filter.tag != "net" || o.filter.grep != "hello" {
		t.Fatalf("options=%+v ok=%v", o, ok)
	}
	o, ok = parseOptions([]string{"export"})
	if !ok || o.follow || !o.export {
		t.Fatalf("snapshot export=%+v", o)
	}
	o, ok = parseOptions([]string{"-text", "-app=A,B", "-level=W", "-tag=net", "-grep=hello", "-polls=6", "export"})
	if !ok || o.follow || o.polls != 6 || !o.filter.matches(entry{
		app: "A", line: vi.ParseLogLine("1 W net hello")}) {
		t.Fatalf("compact options=%+v ok=%v", o, ok)
	}
	o, ok = parseOptions([]string{"-follow", "-snapshot", "export"})
	if !ok || o.follow {
		t.Fatalf("last mode wins: %+v", o)
	}
	for _, args := range [][]string{{"-level", "X"}, {"-app", "../bad"}, {"-polls", "0"}, {"-tag"}, {"unknown"}} {
		if _, ok := parseOptions(args); ok {
			t.Fatalf("accepted %q", args)
		}
	}
}

func texts(rows []entry) []string {
	var out []string
	for _, row := range rows {
		out = append(out, row.text())
	}
	return out
}

func sequenceRing(first, last int) []byte {
	var b strings.Builder
	for i := first; i <= last; i++ {
		b.WriteString(strconv.Itoa(i) + " I net same\n")
	}
	return []byte(b.String())
}
