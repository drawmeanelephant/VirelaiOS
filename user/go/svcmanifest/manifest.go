// Package svcmanifest owns the bounded JSON v1 service document (ADR 0042).
// It has no process, graph, settings or supervisor dependencies.
package svcmanifest

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	Path        = "/host/INIT/SERVICES.JSON"
	MaxBytes    = 8192
	MaxServices = 3
	MaxEdges    = 6
	MaxArgs     = 8
	MaxArgBytes = 255
)

type Manifest struct {
	Version  int
	Services []Service
}

type Service struct {
	Name     string
	Argv     []string
	Requires []string
	Wants    []string
	Restart  Policy
	Class    string
	Caps     []string
	Enabled  bool
}

type Policy struct {
	Restart      string
	BackoffBaseS uint64
	BackoffCapS  uint64
	MaxRestarts  uint32
	Window       uint64
}

type Code string

const (
	Oversize            Code = "oversize"
	InvalidJSON         Code = "invalid-json"
	UnknownKey          Code = "unknown-key"
	DuplicateKey        Code = "duplicate-key"
	NullField           Code = "null-field"
	BadVersion          Code = "bad-version"
	TooManyServices     Code = "too-many-services"
	EmptyServices       Code = "empty-services"
	BadName             Code = "bad-name"
	DuplicateName       Code = "duplicate-name"
	BadArgv             Code = "bad-argv"
	BadPolicy           Code = "bad-policy"
	BadClass            Code = "bad-class"
	BadCaps             Code = "bad-caps"
	BadDependency       Code = "bad-dependency"
	DuplicateDependency Code = "duplicate-dependency"
	TooManyEdges        Code = "too-many-edges"
	BadEnabled          Code = "bad-enabled"
)

// Error names the refusal and its field; it never echoes configuration values.
type Error struct {
	Code  Code
	Field string
}

func (e *Error) Error() string {
	if e.Field == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Field
}

func refuse(code Code, field string) error { return &Error{code, field} }

// Parse returns no partial manifest on refusal. Graph reachability, cycles and
// actual process capacity are deliberately the consumers' responsibilities.
func Parse(data []byte) (Manifest, error) {
	v, err := readJSON(data)
	if err != nil {
		return Manifest{}, err
	}
	root, err := object(v, "manifest", "version", "services")
	if err != nil {
		return Manifest{}, err
	}
	version, ok := unsigned(root["version"])
	if !ok || version != 1 {
		return Manifest{}, refuse(BadVersion, "version")
	}
	rows, ok := root["services"].([]any)
	if !ok {
		return Manifest{}, refuse(InvalidJSON, "services")
	}
	if len(rows) > MaxServices {
		return Manifest{}, refuse(TooManyServices, "services")
	}
	m := Manifest{Version: 1}
	for _, row := range rows {
		s, err := readService(row)
		if err != nil {
			return Manifest{}, err
		}
		m.Services = append(m.Services, s)
	}
	if err := Validate(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func object(v any, field string, keys ...string) (map[string]any, error) {
	o, ok := v.(map[string]any)
	if !ok {
		return nil, refuse(InvalidJSON, field)
	}
	for key := range o {
		known := false
		for _, allowed := range keys {
			if key == allowed {
				known = true
				break
			}
		}
		if !known {
			return nil, refuse(UnknownKey, field)
		}
	}
	return o, nil
}

func unsigned(v any) (uint64, bool) {
	n, ok := v.(jsonNumber)
	if !ok || strings.ContainsAny(string(n), "-.eE") {
		return 0, false
	}
	value, err := strconv.ParseUint(string(n), 10, 64)
	return value, err == nil
}

func stringList(v any, code Code, field string) ([]string, error) {
	if v == nil { // absent optional field, not JSON null (reader refuses null)
		return nil, nil
	}
	values, ok := v.([]any)
	if !ok {
		return nil, refuse(code, field)
	}
	result := make([]string, len(values))
	for i, v := range values {
		result[i], ok = v.(string)
		if !ok {
			return nil, refuse(code, field)
		}
	}
	return result, nil
}

func readService(v any) (Service, error) {
	o, err := object(v, "service", "name", "argv", "requires", "wants", "restart", "class", "caps", "enabled")
	if err != nil {
		return Service{}, err
	}
	s := Service{Enabled: true}
	s.Name, _ = o["name"].(string)
	s.Class, _ = o["class"].(string)
	if v, present := o["enabled"]; present {
		var ok bool
		s.Enabled, ok = v.(bool)
		if !ok {
			return Service{}, refuse(BadEnabled, "enabled")
		}
	}
	for _, field := range []struct {
		key  string
		code Code
		dest *[]string
	}{
		{"argv", BadArgv, &s.Argv}, {"requires", BadDependency, &s.Requires},
		{"wants", BadDependency, &s.Wants}, {"caps", BadCaps, &s.Caps},
	} {
		*field.dest, err = stringList(o[field.key], field.code, field.key)
		if err != nil {
			return Service{}, err
		}
	}
	p, ok := o["restart"].(map[string]any)
	if !ok {
		return Service{}, refuse(BadPolicy, "restart")
	}
	if _, err := object(p, "restart", "restart", "backoff_base_s", "backoff_cap_s", "max_restarts", "window"); err != nil {
		return Service{}, err
	}
	s.Restart.Restart, _ = p["restart"].(string)
	for _, field := range []struct {
		key  string
		dest *uint64
	}{
		{"backoff_base_s", &s.Restart.BackoffBaseS},
		{"backoff_cap_s", &s.Restart.BackoffCapS},
		{"window", &s.Restart.Window},
	} {
		if v, present := p[field.key]; present {
			*field.dest, ok = unsigned(v)
			if !ok {
				return Service{}, refuse(BadPolicy, field.key)
			}
		}
	}
	if v, present := p["max_restarts"]; present {
		n, ok := unsigned(v)
		if !ok || n > 32 {
			return Service{}, refuse(BadPolicy, "max_restarts")
		}
		s.Restart.MaxRestarts = uint32(n)
	}
	return s, nil
}

// Validate applies the same semantic rules to programmatically built values.
// Disabled records remain validated, so enabling one never exposes stale junk.
func Validate(m Manifest) error {
	if m.Version != 1 {
		return refuse(BadVersion, "version")
	}
	if len(m.Services) == 0 {
		return refuse(EmptyServices, "services")
	}
	if len(m.Services) > MaxServices {
		return refuse(TooManyServices, "services")
	}
	names := make(map[string]bool)
	edges := 0
	for _, s := range m.Services {
		if !label(s.Name, 28) {
			return refuse(BadName, "name")
		}
		if names[s.Name] {
			return refuse(DuplicateName, "name")
		}
		names[s.Name] = true
		if len(s.Argv) == 0 || len(s.Argv) > MaxArgs {
			return refuse(BadArgv, "argv")
		}
		for _, arg := range s.Argv {
			if len(arg) > MaxArgBytes || strings.IndexByte(arg, 0) >= 0 || !utf8.ValidString(arg) {
				return refuse(BadArgv, "argv")
			}
		}
		binary := s.Argv[0]
		if !label(binary, MaxArgBytes) ||
			!(strings.HasSuffix(binary, ".ELF") || strings.HasSuffix(binary, ".BIN")) ||
			len(binary) <= 4 {
			return refuse(BadArgv, "argv")
		}
		p := s.Restart
		if p.Restart == "never" {
			if p.BackoffBaseS != 0 || p.BackoffCapS != 0 || p.MaxRestarts != 0 || p.Window != 0 {
				return refuse(BadPolicy, "restart")
			}
		} else if (p.Restart != "always" && p.Restart != "on-failure") ||
			p.BackoffBaseS < 1 || p.BackoffBaseS > p.BackoffCapS || p.BackoffCapS > 3600 ||
			p.MaxRestarts < 1 || p.MaxRestarts > 32 || p.Window < p.BackoffCapS || p.Window > 86400 {
			return refuse(BadPolicy, "restart")
		}
		if s.Class != "boot" && s.Class != "on-demand" {
			return refuse(BadClass, "class")
		}
		deps := make(map[string]bool)
		for _, list := range [][]string{s.Requires, s.Wants} {
			for _, dep := range list {
				if !label(dep, 28) {
					return refuse(BadDependency, "dependency")
				}
				if deps[dep] {
					return refuse(DuplicateDependency, "dependency")
				}
				deps[dep] = true
				edges++
			}
		}
		caps := make(map[string]bool)
		for _, cap := range s.Caps {
			switch cap {
			case "file", "window", "audio", "timer", "memory", "process", "debug", "net", "term":
			default:
				return refuse(BadCaps, "caps")
			}
			if caps[cap] {
				return refuse(BadCaps, "caps")
			}
			caps[cap] = true
		}
	}
	if edges > MaxEdges {
		return refuse(TooManyEdges, "dependency")
	}
	return nil
}

func label(s string, max int) bool {
	if len(s) < 1 || len(s) > max || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
