// Package guard is the gsport source-walk guard: it parses every Go file
// under the adapter tree (user/go/gsport) and the Gostalgia overlay trees
// (tools/go/overlay/gostalgia*) and refuses the POSIX/host routes the card
// names — os/exec, os/signal, x/sys/unix, any syscall or raw vsys use
// outside the one abi seam, a virelai/vi import anywhere but abi, and any
// net.Listen*/net.Dial* transport call (unix or tcp named; all transports
// refused outright — the guest has none, mem:// is the only scheme).
//
// The walk covers new files under those roots automatically: an M95c/d/e
// overlay file lands in a walked directory and is checked without a guard
// edit.
package guard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Violation is one refused source line.
type Violation struct {
	Path string // repo-relative-ish path as walked
	Line int
	Rule string
	What string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", v.Path, v.Line, v.Rule, v.What)
}

// forbiddenImports are import paths no walked file may take. The value is
// the reason reported.
var forbiddenImports = map[string]string{
	"os/exec":               "host processes are not a guest primitive; use gsport/proc",
	"os/signal":             "virelai has no async signal delivery; use gsport/sig",
	"syscall":               "raw syscall use only through gsport/abi",
	"golang.org/x/sys/unix": "POSIX bindings have no meaning on the guest",
	"virelai/vsys":          "raw svc layer only through gsport/abi",
	"virelai/vi":            "vi calls only through gsport/abi (slot evidence stays in one place)",
}

// abiImportOK reports whether path may import virelai/vi / virelai/vsys:
// exactly the abi package itself.
func abiImportOK(path string) bool {
	return strings.Contains(filepath.ToSlash(path), "/gsport/abi/")
}

// CheckFile parses src as filename and returns the violations it finds.
// abiOK bypasses only the virelai/vi and virelai/vsys import rules.
func CheckFile(filename string, src []byte, abiOK bool) []Violation {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return []Violation{{Path: filename, Line: 1, Rule: "parse", What: err.Error()}}
	}
	var out []Violation
	pos := func(p token.Pos) int { return fset.Position(p).Line }

	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		reason, bad := forbiddenImports[path]
		if !bad {
			continue
		}
		if abiOK && (path == "virelai/vi" || path == "virelai/vsys") {
			continue
		}
		out = append(out, Violation{Path: filename, Line: pos(imp.Pos()), Rule: "import", What: path + " — " + reason})
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch {
		case recv.Name == "syscall":
			out = append(out, Violation{Path: filename, Line: pos(call.Pos()), Rule: "call", What: "syscall." + sel.Sel.Name + " — raw syscall use only through gsport/abi"})
		case recv.Name == "vi" && !abiOK:
			out = append(out, Violation{Path: filename, Line: pos(call.Pos()), Rule: "call", What: "vi." + sel.Sel.Name + " — vi calls only through gsport/abi"})
		case recv.Name == "vsys" && !abiOK:
			out = append(out, Violation{Path: filename, Line: pos(call.Pos()), Rule: "call", What: "vsys." + sel.Sel.Name + " — raw svc use only through gsport/abi"})
		case recv.Name == "unix":
			out = append(out, Violation{Path: filename, Line: pos(call.Pos()), Rule: "call", What: "unix." + sel.Sel.Name + " — POSIX bindings have no meaning on the guest"})
		case recv.Name == "signal":
			out = append(out, Violation{Path: filename, Line: pos(call.Pos()), Rule: "call", What: "signal." + sel.Sel.Name + " — virelai has no async signal delivery; use gsport/sig"})
		case recv.Name == "exec":
			out = append(out, Violation{Path: filename, Line: pos(call.Pos()), Rule: "call", What: "exec." + sel.Sel.Name + " — host processes are not a guest primitive; use gsport/proc"})
		case recv.Name == "net" && (strings.HasPrefix(sel.Sel.Name, "Listen") || strings.HasPrefix(sel.Sel.Name, "Dial")):
			out = append(out, Violation{Path: filename, Line: pos(call.Pos()), Rule: "call", What: "net." + sel.Sel.Name + " — no unix/tcp transport on the guest; mem:// via gsport/ipc is the only endpoint"})
		}
		return true
	})
	return out
}

// CheckTree walks root (a directory) and checks every .go file in it.
// Missing roots are skipped — a reserved overlay dir not yet populated is
// not a violation.
func CheckTree(root string) []Violation {
	var out []Violation
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			// Tests are host-only (never compile for the guest) and may
			// legitimately import vi for slot constants.
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			out = append(out, Violation{Path: path, Rule: "read", What: err.Error()})
			return nil
		}
		out = append(out, CheckFile(path, src, abiImportOK(path))...)
		return nil
	})
	return out
}

// CheckRoots checks every listed root, returning all violations.
func CheckRoots(roots ...string) []Violation {
	var out []Violation
	for _, r := range roots {
		out = append(out, CheckTree(r)...)
	}
	return out
}
