package svcmanifest

import (
	"encoding/json"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// json.Valid owns JSON syntax. This bounded reader avoids linking the standard
// reflect-based decoder, whose measured guest ELF exceeds the app size cap.
// Nulls, duplicate keys and non-scalar Unicode escapes refuse the whole input.
type jsonReader struct {
	data []byte
	pos  int
}

type jsonNumber string

func readJSON(data []byte) (any, error) {
	if len(data) > MaxBytes {
		return nil, refuse(Oversize, "manifest")
	}
	if !utf8.Valid(data) || !json.Valid(data) {
		return nil, refuse(InvalidJSON, "manifest")
	}
	r := jsonReader{data: data}
	return r.value(0)
}

func (r *jsonReader) space() {
	for r.pos < len(r.data) {
		switch r.data[r.pos] {
		case ' ', '\t', '\r', '\n':
			r.pos++
		default:
			return
		}
	}
}

func (r *jsonReader) value(depth int) (any, error) {
	if depth > 8 {
		return nil, refuse(InvalidJSON, "nesting")
	}
	r.space()
	switch r.data[r.pos] {
	case '{':
		r.pos++
		o := make(map[string]any)
		r.space()
		if r.data[r.pos] == '}' {
			r.pos++
			return o, nil
		}
		for {
			key, err := r.text()
			if err != nil {
				return nil, err
			}
			if _, exists := o[key]; exists {
				return nil, refuse(DuplicateKey, "object")
			}
			r.space()
			r.pos++ // colon, already checked by json.Valid
			v, err := r.value(depth + 1)
			if err != nil {
				return nil, err
			}
			o[key] = v
			r.space()
			end := r.data[r.pos]
			r.pos++
			if end == '}' {
				return o, nil
			}
			r.space()
		}
	case '[':
		r.pos++
		var a []any
		r.space()
		if r.data[r.pos] == ']' {
			r.pos++
			return a, nil
		}
		for {
			v, err := r.value(depth + 1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
			r.space()
			end := r.data[r.pos]
			r.pos++
			if end == ']' {
				return a, nil
			}
		}
	case '"':
		return r.text()
	case 'n':
		return nil, refuse(NullField, "value")
	case 't':
		r.pos += 4
		return true, nil
	case 'f':
		r.pos += 5
		return false, nil
	default:
		start := r.pos
		for r.pos < len(r.data) {
			c := r.data[r.pos]
			if !(c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E') {
				break
			}
			r.pos++
		}
		return jsonNumber(r.data[start:r.pos]), nil
	}
}

func (r *jsonReader) hexRune() rune {
	n, _ := strconv.ParseUint(string(r.data[r.pos:r.pos+4]), 16, 16)
	r.pos += 4
	return rune(n)
}

func (r *jsonReader) text() (string, error) {
	r.pos++ // opening quote
	var b []byte
	for r.data[r.pos] != '"' {
		c := r.data[r.pos]
		r.pos++
		if c != '\\' {
			b = append(b, c)
			continue
		}
		c = r.data[r.pos]
		r.pos++
		switch c {
		case '"', '\\', '/':
			b = append(b, c)
		case 'b':
			b = append(b, '\b')
		case 'f':
			b = append(b, '\f')
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case 'u':
			v := r.hexRune()
			if utf16.IsSurrogate(v) {
				if v > 0xdbff || r.pos+6 > len(r.data) ||
					r.data[r.pos] != '\\' || r.data[r.pos+1] != 'u' {
					return "", refuse(InvalidJSON, "unicode")
				}
				r.pos += 2
				low := r.hexRune()
				if low < 0xdc00 || low > 0xdfff {
					return "", refuse(InvalidJSON, "unicode")
				}
				v = utf16.DecodeRune(v, low)
			}
			b = utf8.AppendRune(b, v)
		}
	}
	r.pos++
	return string(b), nil
}
