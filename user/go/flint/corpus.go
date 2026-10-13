package main

import (
	"sort"
	"strconv"
	"strings"

	"virelai/flint/parser"
	"virelai/vi"
)

// parseCorpus walks <dir>/go and <dir>/zig, parses every file into §3
// Atoms, writes the Render() receipt to <dir>/../FLINTC.OUT (the share
// root when dir is /host/FLINTC) and prints the marker line
// `flint: parse files=N atoms=M parse_errors=E`. A file that will not
// parse is counted in parse_errors, never silently dropped — the gate's
// atom count must equal MANIFEST.json's exactly.
func parseCorpus(dir string) {
	dir = strings.TrimSuffix(dir, "/")
	var atoms []parser.Atom
	files, errs := 0, 0
	for _, sub := range []string{"go", "zig"} {
		ext := "." + sub
		items, err := vi.ListDirAt(listCanon(dir) + "/" + sub)
		if err != nil {
			vi.ConsoleLine("flint: list " + sub + ": " + err.Error())
			errs++
			continue
		}
		names := make([]string, 0, len(items))
		for _, it := range items {
			if !it.IsDir && strings.HasSuffix(it.Name, ext) {
				names = append(names, it.Name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			files++
			src, rc := vi.ReadFileAll(dir+"/"+sub+"/"+name, vi.MaxFileBytes)
			if rc < 0 {
				vi.ConsoleLine("flint: read " + sub + "/" + name + " rc=" + strconv.FormatInt(rc, 10))
				errs++
				continue
			}
			p, ok := parser.ForPath(sub + "/" + name)
			if !ok {
				errs++
				continue
			}
			as, err := p.Parse(sub+"/"+name, src)
			if err != nil {
				vi.ConsoleLine("flint: " + err.Error())
				errs++
				continue
			}
			atoms = append(atoms, as...)
		}
	}
	out := parser.Render(atoms)
	if parent := dir[:strings.LastIndexByte(dir, '/')]; parent != "" {
		if rc := vi.WriteFileSafe(parent+"/FLINTC.OUT", out); rc < 0 {
			vi.ConsoleLine("flint: write FLINTC.OUT rc=" + strconv.FormatInt(rc, 10))
			errs++
		}
	}
	vi.ConsoleLine("flint: parse files=" + strconv.Itoa(files) +
		" atoms=" + strconv.Itoa(len(atoms)) +
		" parse_errors=" + strconv.Itoa(errs))
	if errs != 0 {
		vi.Exit(1)
	}
}

// listCanon converts a /host-rooted path into the shell-canonical form
// ListDirAt wants (/-rooted where / is the share root).
func listCanon(dir string) string {
	switch {
	case dir == "" || dir == "/host":
		return "/"
	case strings.HasPrefix(dir, "/host/"):
		return dir[len("/host"):]
	}
	return dir
}
