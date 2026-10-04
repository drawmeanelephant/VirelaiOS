package vectorconsumer

import (
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"virelai/vector"
)

func buffers() (vector.Storage, vector.Target, vector.Workspace) {
	return vector.Storage{Commands: make([]vector.Command, vector.MaxCommands+1), Paints: make([]vector.Paint, vector.MaxPaints+1)},
		vector.Target{Pix: make([]uint32, 1024*1536+16), Width: 64, Height: 64, Stride: 64},
		vector.Workspace{Bytes: make([]byte, vector.WorkspaceSize)}
}

func TestIndependentImport(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "consumer.go", nil, parser.ImportsOnly)
	if err != nil || len(f.Imports) != 1 || f.Imports[0].Path.Value != `"virelai/vector"` {
		t.Fatal("consumer must import only virelai/vector", err)
	}
}

func TestAnalyticImmutableAndAllocationFree(t *testing.T) {
	out, dst, ws := buffers()
	for _, id := range Names {
		s, w, h, stride, bg := Build(id, out)
		dst.Width, dst.Height, dst.Stride = w, h, stride
		commands := append([]vector.Command(nil), s.Commands...)
		paints := append([]vector.Paint(nil), s.Paints...)
		run := func() {
			for i := range dst.Pix {
				dst.Pix[i] = bg
			}
			b := vector.Budget{Max: vector.MaxWork}
			stats, f := vector.Rasterize(s, dst, ws, &b)
			if f.Code != vector.OK || stats.Work != b.Used || !Check(id, dst) {
				t.Fatalf("%s: %+v %+v analytic mismatch", id, stats, f)
			}
			for _, p := range dst.Pix[stride*h:] {
				if p != bg {
					t.Fatal("tail overwritten")
				}
			}
		}
		run()
		// Full-page repetitions are covered in the guest; small calls isolate
		// allocation semantics without an unnecessarily slow host test.
		if id != "page" && testing.AllocsPerRun(10, run) != 0 {
			t.Fatalf("%s allocated", id)
		}
		if !reflect.DeepEqual(commands, s.Commands) || !reflect.DeepEqual(paints, s.Paints) {
			t.Fatal("input mutated", id)
		}
	}
}

func TestPreflightFailuresAndRecovery(t *testing.T) {
	out, dst, ws := buffers()
	if !Failures(out, dst, ws) {
		t.Fatal("failure suite or unchanged destination")
	}
	if n := testing.AllocsPerRun(2, func() {
		if !Failures(out, dst, ws) {
			t.Fatal("reuse failed")
		}
	}); n != 0 {
		t.Fatal("failure calls allocate", n)
	}
}
