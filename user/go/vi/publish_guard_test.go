package vi

// M81e2 (#1787) — the recursion guard.
//
// M81e converted every shipped writer to vi.WriteFileSafe one at a time, and
// each conversion was a decision somebody had to make. The failure mode for
// that kind of work is not doing it badly; it is the NEXT app opening
// ModeWrite|ModeCreate, calling FileWriteAll, and shipping a file that a
// crash can tear. Nothing in the compiler objects — the code is correct, the
// contract is just quietly gone.
//
// So this test makes the rule mechanical. It parses every non-test Go file
// in the tree and fails on an in-place writer: a vi.FileOpen whose flags
// carry ModeWrite on a path the writer MEANS to leave as the live file, and
// any vi.FileTruncate on a final path. vi.WriteFileSafe's `~` sacrificial
// temp is the one legitimate truncation and is exempt by construction.
//
// The exemptions are RULES, not a list of filenames, because a filename
// allowlist is exactly the thing that rots: it needs editing every time a
// file is renamed, so it gets widened instead, and the guard quietly stops
// guarding. Each rule below states WHY the shape is not a torn-file hazard:
//
//   - vi.FileAppend / an open carrying ModeAppend: an append never
//     truncates a file it is not rewriting. Nothing already in the file is
//     at risk, so there is no partial-file hazard to prevent.
//   - an open on /dev/tty: a bound terminal handle, painted through, not a
//     stored file. (ModeRead|ModeWrite on the tty is how the front-ends
//     attach.)
//   - ModeDir: a mkdir, not a write.
//   - user/go/selftest: GOSELF is the kernel/syscall conformance harness —
//     its whole job is to call individual slots directly and report what
//     each one returned. It is exempt by the same rule M81e used when it
//     audited the writers, and the class-B go-selftest gate is what holds
//     it to account.
//   - the vi package's own WriteFileSafe: the `~` temp IS the one file a
//     publish is allowed to truncate, because it is the sacrificial copy and
//     it is renamed into place before anyone reads the live path.
//
// The allowlist below is EMPTY on purpose. M81e1 removed the last three
// in-place writers and this card removed the shell hook, so a non-empty
// allowlist would mean the rule had already started to rot. TestTheGuardBites
// proves the test is not vacuous.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The tree this guard walks. vi/ lives at user/go/vi, so the module root is
// one level up.
const guardRoot = ".."

// allowedInPlacePublish is deliberately empty. See the file comment: every
// exemption is a rule expressed in the checker, so that a renamed or new
// file cannot need an entry here. If you find yourself adding a path to this
// map, the shape it names is not covered by a rule — extend the rule instead.
var allowedInPlacePublish = map[string]bool{}

// guardExempt reports whether a file is exempt from the scan entirely, with
// the reason. Kept separate from the AST rules so the "why" is visible at
// the call site rather than buried in a boolean expression.
func guardExempt(path string, isTest bool) (bool, string) {
	if isTest {
		return true, "test file"
	}
	// GOSELF calls individual slots on purpose; go-selftest is its gate.
	if strings.Contains(filepath.ToSlash(path), "/selftest/") {
		return true, "selftest (GOSELF drives slots directly; gated by go-selftest)"
	}
	// The publish primitive itself. Not a blanket package exemption: the
	// checker still inspects this file, it just treats a `~` temp open as
	// the sacrificial copy it is. See guardIsTempPath.
	return false, ""
}

// isTTYOpen reports whether an open names the bound terminal. The front-ends
// keep the path in a `ttyPath` const and paint through it, so the name has
// to be RESOLVED through the file's consts — a substring test on the
// identifier "ttyPath" alone would exempt nothing.
func isTTYOpen(path string, consts map[string]string) bool {
	if strings.Contains(path, "/dev/tty") {
		return true
	}
	seen := 0
	for name := path; name != "" && seen < 8; seen++ {
		v, ok := consts[name]
		if !ok {
			return false
		}
		if strings.Contains(v, "/dev/tty") {
			return true
		}
		name = v
	}
	return false
}

// guardIsTempPath reports whether a path expression is vi.WriteFileSafe's
// sacrificial temp — the one open allowed to truncate in place. It matches
// the `path + "~"` concat the primitive builds.
func guardIsTempPath(e ast.Expr) bool {
	b, ok := e.(*ast.BinaryExpr)
	if !ok || b.Op != token.ADD {
		return false
	}
	lit, ok := b.Y.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && strings.Contains(lit.Value, "~")
}

// modeWord is a vi.Mode* set plus the verdict on whether the expression it
// came from can TRUNCATE — ModeWrite present, ModeAppend absent. The two are
// kept separate because a UNION of mode names loses the answer: a variable
// assigned ModeWrite|ModeCreate in one branch and ModeAppend in another
// contains the name ModeAppend and still truncates whenever the second
// branch is not taken.
type modeWord struct {
	names      map[string]bool
	truncating bool
}

// truncates reports whether this mode word opens a file for writing in
// place. A set carrying both ModeWrite and ModeAppend is an append and
// cannot tear what is already in the file.
func (m modeWord) truncates() bool {
	return m.truncating
}

func (m modeWord) has(name string) bool { return m.names[name] }

// modeFlagsIn returns the vi.Mode* names named anywhere in an expression, so
// `flags := vi.ModeWrite | vi.ModeCreate` followed by
// `vi.FileOpen(path, flags)` is understood as a write-create open.
func modeFlagsIn(e ast.Expr) modeWord {
	out := modeWord{names: map[string]bool{}}
	ast.Inspect(e, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "vi" {
			out.names[sel.Sel.Name] = true
		}
		return true
	})
	out.truncating = out.names["ModeWrite"] && !out.names["ModeAppend"]
	return out
}

// resolvePathName returns a readable name for a path argument so a failure
// message can quote it, following a bare identifier into the file's consts
// when there is one.
func resolvePathName(e ast.Expr, consts map[string]string) string {
	switch v := e.(type) {
	case *ast.Ident:
		if s, ok := consts[v.Name]; ok {
			return s
		}
		return v.Name
	case *ast.BasicLit:
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return v.Value
		}
		return s
	case *ast.BinaryExpr:
		return resolvePathName(v.X, consts) + " + " + resolvePathName(v.Y, consts)
	}
	return "<expr>"
}

// fileConsts collects the file's single-valued string consts and vars, so an
// identifier used as a path can be resolved to the literal it stands for.
func fileConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if s, err := strconv.Unquote(lit.Value); err == nil {
				out[vs.Names[0].Name] = s
			}
		}
	}
	return out
}

// checkFile parses one file and returns a human-readable reason per
// violation found.
func checkFile(fset *token.FileSet, path string) []string {
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// A file that does not parse is a different failure, reported by
		// the build. Do not mask it as a guard violation.
		return nil
	}
	var bad []string
	consts := fileConsts(f)
	report := func(pos token.Pos, format string, args ...any) {
		bad = append(bad, path+":"+fset.Position(pos).String()+": "+
			fmt.Sprintf(format, args...))
	}

	// A `flags := ...` binding is visible to the FileOpen that uses it, so
	// collect them first: this is what lets the guard see through the local
	// variable the two shell hooks used to build their mode word.
	//
	// `truncating` records whether ANY assignment to the variable names
	// ModeWrite WITHOUT ModeAppend. That per-assignment test is the point:
	// the old shell hook wrote `flags := ModeWrite|ModeCreate` and then
	// `if appendMode { flags |= ModeAppend }`, so a union of the mode names
	// would "prove" it was an append and quietly exempt the very writer
	// this card removed. Merging a conditional OR into the mode word claims
	// a proof the code does not carry — the open CAN truncate. A variable is
	// append-safe only if EVERY assignment to it carries ModeAppend.
	flagVars := map[string]modeWord{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}
			id, ok := as.Lhs[0].(*ast.Ident)
			if !ok || id.Name == "_" {
				return true
			}
			m := modeFlagsIn(as.Rhs[0])
			if len(m.names) == 0 {
				return true
			}
			w := flagVars[id.Name]
			if w.names == nil {
				w.names = map[string]bool{}
			}
			for k := range m.names {
				w.names[k] = true
			}
			if m.truncates() {
				w.truncating = true
			}
			flagVars[id.Name] = w
			return true
		})
	}

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		// vi.WriteFileSafe's own temp is the sanctioned truncation.
		inPublish := fd.Name.Name == "WriteFileSafe"
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "vi" {
				return true
			}

			switch sel.Sel.Name {
			case "FileTruncate":
				// Truncating a handle whose path is the LIVE file is the
				// hazard in its purest form: the file is shortened in
				// place, so a crash mid-way leaves a short file that looks
				// complete.
				report(call.Pos(),
					"vi.FileTruncate on a live file — publish through "+
						"vi.WriteFileSafe (or vi.WriteFilePublish) instead")
			case "FileOpen":
				if len(call.Args) < 2 {
					return true
				}
				flags := modeFlagsIn(call.Args[1])
				if id, ok := call.Args[1].(*ast.Ident); ok && len(flags.names) == 0 {
					// The mode word was bound to a local first; take the
					// per-assignment verdict, not a union of names.
					w, bound := flagVars[id.Name]
					if !bound || !w.truncates() {
						return true
					}
					flags = w
				}
				if !flags.truncates() {
					return true // a read, a create-only, or an append
				}
				if guardIsTempPath(call.Args[0]) {
					return true // the `~` sacrificial copy
				}
				if inPublish {
					return true
				}
				name := resolvePathName(call.Args[0], consts)
				if isTTYOpen(name, consts) {
					return true // a bound terminal handle, not a stored file
				}
				if flags.has("ModeDir") {
					return true // a mkdir
				}
				report(call.Pos(),
					"in-place writer: vi.FileOpen(%q, ModeWrite...) then "+
						"FileWriteAll leaves a file a crash can tear — "+
						"publish through vi.WriteFileSafe (replace) or "+
						"vi.FileAppend (append)", name)
			}
			return true
		})
	}
	return bad
}

// TestNoInPlaceFileWriters is the guard. It fails on any shipped writer that
// opens a live path for writing in place.
func TestNoInPlaceFileWriters(t *testing.T) {
	fset := token.NewFileSet()
	var violations []string

	err := filepath.Walk(guardRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, rerr := filepath.Rel(guardRoot, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if allowedInPlacePublish[rel] {
			t.Errorf("publish guard: %s is in the allowlist, which must stay "+
				"empty — express the exemption as a rule in this file "+
				"instead", rel)
			return nil
		}
		if exempt, _ := guardExempt(rel, strings.HasSuffix(path, "_test.go")); exempt {
			return nil
		}
		violations = append(violations, checkFile(fset, path)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", guardRoot, err)
	}
	if len(violations) > 0 {
		t.Errorf("in-place file writers found (M81e2 #1787 — every shipped "+
			"writer publishes crash-safe):\n  %s\n\n"+
			"An in-place writer can be torn by a crash: the file is "+
			"truncated and rewritten in place, so a failure or power loss "+
			"leaves a PARTIAL file that later readers cannot tell from a "+
			"complete one. Replace it with vi.WriteFileSafe (when the body "+
			"is fully in memory) or vi.FileAppend (when it is an append). "+
			"If this shape is genuinely not a hazard, add a RULE to this "+
			"guard that says why — not a filename to an allowlist that must "+
			"stay empty.",
			strings.Join(violations, "\n  "))
	}
}

// TestTheGuardBites proves the guard is not vacuous: a file that looks
// exactly like the shell hook M81e2 removed must fail it. Without this, a
// checker that silently stopped matching anything would still report a green
// suite, which is the one failure mode a guard cannot be allowed to have.
//
// The fixture is written to a temp dir and scanned with the same checkFile
// the real tree uses, so what is proven is the checker, not a copy of it.
func TestTheGuardBites(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // substring the violation must contain
	}{
		{
			// The exact shape goshHost.WriteFile had: build a mode word in
			// a local, open the live path with it, write in place.
			name: "the shell hook M81e2 removed",
			src: `package fixture

import "virelai/vi"

func (g *host) WriteFile(path string, b []byte, appendMode bool) error {
	flags := vi.ModeWrite | vi.ModeCreate
	if appendMode {
		flags |= vi.ModeAppend
	}
	h, r := vi.FileOpen(path, flags)
	if r < 0 {
		return nil
	}
	defer vi.FileClose(uint32(h))
	vi.FileWriteAll(uint32(h), b)
	return nil
}
`,
			want: "in-place writer",
		},
		{
			// A future writer that skips the local variable entirely.
			name: "inline mode word",
			src: `package fixture

import "virelai/vi"

func save(path string, b []byte) {
	h, r := vi.FileOpen(path, vi.ModeWrite|vi.ModeCreate)
	if r < 0 {
		return
	}
	vi.FileWriteAll(uint32(h), b)
	vi.FileClose(uint32(h))
}
`,
			want: "in-place writer",
		},
		{
			// Truncation in its purest form.
			name: "truncate in place",
			src: `package fixture

import "virelai/vi"

func compact(path string) {
	h, r := vi.FileOpen(path, vi.ModeWrite|vi.ModeCreate)
	if r < 0 {
		return
	}
	vi.FileWriteAll(uint32(h), []byte("x"))
	vi.FileTruncate(uint32(h), 0)
	vi.FileClose(uint32(h))
}
`,
			want: "vi.FileTruncate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "fixture.go")
			if err := os.WriteFile(p, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			got := checkFile(token.NewFileSet(), p)
			if len(got) == 0 {
				t.Fatalf("guard did NOT fire on %q — the guard is "+
					"vacuous and every other assertion in this file is "+
					"worthless:\n%s", tc.name, tc.src)
			}
			if !strings.Contains(strings.Join(got, "\n"), tc.want) {
				t.Errorf("guard fired on %q but not for the expected "+
					"reason %q:\n  %s", tc.name, tc.want,
					strings.Join(got, "\n  "))
			}
		})
	}
}

// TestTheGuardExemptsTheLegalShapes is the other half of "the guard is
// honest": a guard that fires on everything is as useless as one that fires
// on nothing. These are the shapes the rules exempt, and they must stay
// green or the exemptions will get "fixed" by deleting the rule.
func TestTheGuardExemptsTheLegalShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "the safe publish's own temp",
			src: `package vi

func WriteFileSafe(path string, b []byte) int64 {
	tmp := path + "~"
	h, r := FileOpen(tmp, ModeWrite|ModeCreate)
	if r < 0 {
		return r
	}
	FileWriteAll(uint32(h), b)
	FileSync(uint32(h))
	FileClose(uint32(h))
	return 0
}
`,
		},
		{
			name: "an append is not a rewrite",
			src: `package vi

func WriteFilePublish(path string, b []byte, appendMode bool) int64 {
	if !appendMode {
		return WriteFileSafe(path, b)
	}
	h, r := FileOpen(path, ModeWrite|ModeCreate|ModeAppend)
	if r < 0 {
		return r
	}
	defer FileClose(uint32(h))
	FileWriteAll(uint32(h), b)
	return 0
}
`,
		},
		{
			name: "a tty attach is not a stored file",
			src: `package sh

import "virelai/vi"

const ttyPath = "/dev/tty"

func attach() {
	h, rc := vi.FileOpen(ttyPath, vi.ModeRead|vi.ModeWrite)
	if rc < 0 {
		return
	}
	vi.FileClose(uint32(h))
}
`,
		},
		{
			name: "a mkdir is not a write",
			src: `package git

import "virelai/vi"

func mkdir(path string) {
	vi.FileOpen(path, vi.ModeWrite|vi.ModeCreate|vi.ModeDir)
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "fixture.go")
			if err := os.WriteFile(p, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := checkFile(token.NewFileSet(), p); len(got) != 0 {
				t.Errorf("guard fired on a legal shape (%q), so the "+
					"exemptions are wrong — someone will delete the rule "+
					"instead of fixing it:\n  %s", tc.name,
					strings.Join(got, "\n  "))
			}
		})
	}
}
