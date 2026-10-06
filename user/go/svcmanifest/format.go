package svcmanifest

import (
	"strconv"
	"strings"
)

// Format emits the complete v1 document in a readable, stable field order.
// It intentionally avoids encoding/json's reflect-based encoder in the guest.
func Format(m Manifest) ([]byte, error) {
	if err := Validate(m); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("{\n  \"version\": 1,\n  \"services\": [\n")
	for i, s := range m.Services {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString("    {\n      \"name\": ")
		quote(&b, s.Name)
		b.WriteString(",\n      \"argv\": ")
		list(&b, s.Argv)
		b.WriteString(",\n      \"requires\": ")
		list(&b, s.Requires)
		b.WriteString(",\n      \"wants\": ")
		list(&b, s.Wants)
		b.WriteString(",\n      \"restart\": {\"restart\": ")
		quote(&b, s.Restart.Restart)
		b.WriteString(", \"backoff_base_s\": " + strconv.FormatUint(s.Restart.BackoffBaseS, 10))
		b.WriteString(", \"backoff_cap_s\": " + strconv.FormatUint(s.Restart.BackoffCapS, 10))
		b.WriteString(", \"max_restarts\": " + strconv.FormatUint(uint64(s.Restart.MaxRestarts), 10))
		b.WriteString(", \"window\": " + strconv.FormatUint(s.Restart.Window, 10))
		b.WriteString("},\n      \"class\": ")
		quote(&b, s.Class)
		b.WriteString(",\n      \"caps\": ")
		list(&b, s.Caps)
		b.WriteString(",\n      \"enabled\": ")
		b.WriteString(strconv.FormatBool(s.Enabled))
		b.WriteString("\n    }")
	}
	b.WriteString("\n  ]\n}\n")
	if b.Len() > MaxBytes {
		return nil, refuse(Oversize, "manifest")
	}
	return []byte(b.String()), nil
}

func list(b *strings.Builder, values []string) {
	b.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		quote(b, value)
	}
	b.WriteByte(']')
}

func quote(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20:
			b.WriteString("\\u00")
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
}
