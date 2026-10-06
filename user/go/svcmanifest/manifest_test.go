package svcmanifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var fixtures = []struct {
	name string
	code Code
}{
	{"seat.json", ""},
	{"one-shot.json", ""},
	{"on-failure.json", ""},
	{"negative-oversize.json", Oversize},
	{"negative-invalid-json.json", InvalidJSON},
	{"negative-unknown-key.json", UnknownKey},
	{"negative-duplicate-key.json", DuplicateKey},
	{"negative-null-field.json", NullField},
	{"negative-bad-version.json", BadVersion},
	{"negative-too-many-services.json", TooManyServices},
	{"negative-empty-services.json", EmptyServices},
	{"negative-bad-name.json", BadName},
	{"negative-duplicate-name.json", DuplicateName},
	{"negative-bad-argv.json", BadArgv},
	{"negative-bad-policy.json", BadPolicy},
	{"negative-bad-class.json", BadClass},
	{"negative-bad-caps.json", BadCaps},
	{"negative-bad-dependency.json", BadDependency},
	{"negative-duplicate-dependency.json", DuplicateDependency},
	{"negative-too-many-edges.json", TooManyEdges},
	{"negative-bad-enabled.json", BadEnabled},
}

func TestFixtures(t *testing.T) {
	dir := "../../../tests/fixtures/init/manifests"
	covered := make(map[string]bool)
	for _, fixture := range fixtures {
		covered[fixture.name] = true
		t.Run(fixture.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, fixture.name))
			if err != nil {
				t.Fatal(err)
			}
			m, err := Parse(data)
			if fixture.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				if m.Version != 1 || len(m.Services) != 1 || !m.Services[0].Enabled {
					t.Fatalf("lost valid service: %+v", m)
				}
			} else {
				assertCode(t, err, fixture.code)
			}
		})
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !covered[filepath.Base(file)] {
			t.Errorf("fixture without assertion: %s", file)
		}
	}
}

func assertCode(t *testing.T, err error, want Code) {
	t.Helper()
	var refusal *Error
	if !errors.As(err, &refusal) || refusal.Code != want {
		t.Fatalf("refusal = %v, want %s", err, want)
	}
}

func validManifest() Manifest {
	return Manifest{Version: 1, Services: []Service{{
		Name: "a", Argv: []string{"USER.BIN"}, Class: "boot",
		Restart: Policy{Restart: "never"}, Enabled: true,
	}}}
}

func TestSemanticBounds(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Service)
		code   Code
	}{
		{"eight-args", func(s *Service) { s.Argv = append(s.Argv, make([]string, 7)...) }, ""},
		{"nine-args", func(s *Service) { s.Argv = append(s.Argv, make([]string, 8)...) }, BadArgv},
		{"255-bytes", func(s *Service) { s.Argv = append(s.Argv, strings.Repeat("x", 255)) }, ""},
		{"256-bytes", func(s *Service) { s.Argv = append(s.Argv, strings.Repeat("x", 256)) }, BadArgv},
		{"utf8-byte-bound", func(s *Service) { s.Argv = append(s.Argv, strings.Repeat("é", 128)) }, BadArgv},
		{"nul-arg", func(s *Service) { s.Argv = append(s.Argv, "a\x00b") }, BadArgv},
		{"bad-utf8-arg", func(s *Service) { s.Argv = append(s.Argv, "\xff") }, BadArgv},
		{"no-argv", func(s *Service) { s.Argv = nil }, BadArgv},
		{"path-binary", func(s *Service) { s.Argv[0] = "/host/USER.BIN" }, BadArgv},
		{"no-binary", func(s *Service) { s.Argv[0] = ".ELF" }, BadArgv},
		{"wrong-extension", func(s *Service) { s.Argv[0] = "USER.EXE" }, BadArgv},
		{"name-28", func(s *Service) { s.Name = strings.Repeat("a", 28) }, ""},
		{"name-29", func(s *Service) { s.Name = strings.Repeat("a", 29) }, BadName},
		{"dot", func(s *Service) { s.Name = "." }, BadName},
		{"dotdot", func(s *Service) { s.Name = ".." }, BadName},
		{"disabled-invalid", func(s *Service) { s.Enabled = false; s.Class = "" }, BadClass},
		{"duplicate-caps", func(s *Service) { s.Caps = []string{"net", "net"} }, BadCaps},
		{"all-caps", func(s *Service) {
			s.Caps = []string{"file", "window", "audio", "timer", "memory", "process", "debug", "net", "term"}
		}, ""},
		{"duplicate-requires", func(s *Service) { s.Requires = []string{"b", "b"} }, DuplicateDependency},
		{"graph-defers-required-cycle", func(s *Service) { s.Requires = []string{"a"} }, ""},
		{"missing-want", func(s *Service) { s.Wants = []string{"missing"} }, ""},
		{"six-edges", func(s *Service) { s.Wants = []string{"b", "c", "d", "e", "f", "g"} }, ""},
		{"never-backoff", func(s *Service) { s.Restart.BackoffBaseS = 1 }, BadPolicy},
		{"restart-minimum", func(s *Service) { s.Restart = Policy{"always", 1, 1, 1, 1} }, ""},
		{"restart-maximum", func(s *Service) { s.Restart = Policy{"on-failure", 3600, 3600, 32, 86400} }, ""},
		{"zero-base", func(s *Service) { s.Restart = Policy{"always", 0, 1, 1, 1} }, BadPolicy},
		{"cap-overflow", func(s *Service) { s.Restart = Policy{"always", 1, 3601, 1, 86400} }, BadPolicy},
		{"base-over-cap", func(s *Service) { s.Restart = Policy{"always", 2, 1, 1, 1} }, BadPolicy},
		{"zero-restarts", func(s *Service) { s.Restart = Policy{"always", 1, 1, 0, 1} }, BadPolicy},
		{"restart-overflow", func(s *Service) { s.Restart = Policy{"always", 1, 1, 33, 1} }, BadPolicy},
		{"window-under-cap", func(s *Service) { s.Restart = Policy{"always", 1, 2, 1, 1} }, BadPolicy},
		{"window-overflow", func(s *Service) { s.Restart = Policy{"always", 1, 1, 1, 86401} }, BadPolicy},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := validManifest()
			test.change(&m.Services[0])
			err := Validate(m)
			if test.code != "" {
				assertCode(t, err, test.code)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStrictDocument(t *testing.T) {
	const row = `{"name":"a","argv":["USER.BIN"],"class":"boot","restart":{"restart":"never"}}`
	wrap := func(row string) string { return `{"version":1,"services":[` + row + `]}` }
	tests := []struct {
		name, data string
		code       Code
	}{
		{"missing-version", `{"services":[]}`, BadVersion},
		{"fractional-version", `{"version":1.0,"services":[]}`, BadVersion},
		{"case-sensitive-key", `{"Version":1,"services":[]}`, UnknownKey},
		{"root-array", `[]`, InvalidJSON},
		{"services-object", `{"version":1,"services":{}}`, InvalidJSON},
		{"trailing-document", wrap(row) + `{}`, InvalidJSON},
		{"comment", `// comment` + wrap(row), InvalidJSON},
		{"unknown-policy-key", wrap(strings.Replace(row, `"never"`, `"never","jitter":1`, 1)), UnknownKey},
		{"duplicate-escaped-key", `{"version":1,"\u0076ersion":1,"services":[]}`, DuplicateKey},
		{"null-in-array", wrap(strings.Replace(row, `"USER.BIN"`, `null`, 1)), NullField},
		{"wrong-argv-type", wrap(strings.Replace(row, `["USER.BIN"]`, `"USER.BIN"`, 1)), BadArgv},
		{"wrong-arg-type", wrap(strings.Replace(row, `"USER.BIN"`, `5`, 1)), BadArgv},
		{"missing-policy", wrap(`{"name":"a","argv":["USER.BIN"],"class":"boot"}`), BadPolicy},
		{"wrong-policy-type", wrap(strings.Replace(row, `{"restart":"never"}`, `"never"`, 1)), BadPolicy},
		{"fractional-policy", wrap(strings.Replace(row, `"never"`, `"never","window":0.5`, 1)), BadPolicy},
		{"negative-policy", wrap(strings.Replace(row, `"never"`, `"never","window":-1`, 1)), BadPolicy},
		{"overflow-policy", wrap(strings.Replace(row, `"never"`, `"never","window":18446744073709551616`, 1)), BadPolicy},
		{"fractional-restarts", wrap(strings.Replace(row, `"never"`, `"never","max_restarts":1.0`, 1)), BadPolicy},
		{"unpaired-surrogate", wrap(strings.Replace(row, `"USER.BIN"`, `"USER.BIN","\ud800"`, 1)), InvalidJSON},
		{"bad-utf8", wrap(strings.Replace(row, `"a"`, "\"\xff\"", 1)), InvalidJSON},
		{"too-deep", strings.Repeat("[", 10) + "1" + strings.Repeat("]", 10), InvalidJSON},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m, err := Parse([]byte(test.data))
			assertCode(t, err, test.code)
			if len(m.Services) != 0 {
				t.Fatal("partial manifest on refusal")
			}
		})
	}
	data := wrap(row)
	if _, err := Parse([]byte(data + strings.Repeat(" ", MaxBytes-len(data)))); err != nil {
		t.Fatalf("exact byte cap: %v", err)
	}
	assertCode(t, func() error {
		_, err := Parse([]byte(data + strings.Repeat(" ", MaxBytes-len(data)+1)))
		return err
	}(), Oversize)
	disabled := strings.Replace(row, `"class":"boot"`, `"class":"boot","enabled":false`, 1)
	m, err := Parse([]byte(wrap(disabled)))
	if err != nil || m.Services[0].Enabled {
		t.Fatalf("explicit disabled lost: %+v, %v", m, err)
	}
}

func TestStringDecoding(t *testing.T) {
	for _, source := range []string{
		`""`, `"abc"`, `"\"\\\/\b\f\n\r\t"`, `"\u0041"`, `"\ud83d\ude00"`,
		`"é 😀"`, `"\u007f\u0080\u00ff"`, `"\\ud800"`,
	} {
		t.Run(source, func(t *testing.T) {
			var want string
			if err := json.Unmarshal([]byte(source), &want); err != nil {
				t.Fatal(err)
			}
			got, err := readJSON([]byte(source))
			if err != nil || got != want {
				t.Fatalf("decoded %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	m := validManifest()
	m.Services[0].Argv = append(m.Services[0].Argv, "quotes\" slash\\ / \x01\té😀")
	data, err := Format(m)
	if err != nil || !json.Valid(data) {
		t.Fatalf("invalid formatted JSON: %q, %v", data, err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Format(parsed)
	if err != nil || string(second) != string(data) {
		t.Fatalf("format not stable: %v", err)
	}
	if !reflect.DeepEqual(parsed.Services[0].Argv, m.Services[0].Argv) {
		t.Fatal("argv changed on round trip")
	}
	m.Services[0].Argv = []string{"USER.BIN"}
	for i := 0; i < 7; i++ {
		m.Services[0].Argv = append(m.Services[0].Argv, strings.Repeat("\x01", MaxArgBytes))
	}
	_, err = Format(m)
	assertCode(t, err, Oversize)
}

func FuzzParse(f *testing.F) {
	for _, fixture := range fixtures {
		data, err := os.ReadFile(filepath.Join("../../../tests/fixtures/init/manifests", fixture.name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(`{"version":1,"services":[{"name":"a","argv":["USER.BIN","\ud83d\ude00"],"class":"boot","restart":{"restart":"never"}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Parse(data)
		if err != nil {
			var refusal *Error
			if !errors.As(err, &refusal) || len(m.Services) != 0 {
				t.Fatalf("unnamed or partial refusal: %v", err)
			}
			return
		}
		if !json.Valid(data) || Validate(m) != nil {
			t.Fatal("accepted invalid input")
		}
		formatted, err := Format(m)
		if err != nil {
			assertCode(t, err, Oversize)
			return
		}
		again, err := Parse(formatted)
		if err != nil {
			t.Fatal(err)
		}
		next, err := Format(again)
		if err != nil || string(next) != string(formatted) {
			t.Fatal("unstable round trip")
		}
	})
}
