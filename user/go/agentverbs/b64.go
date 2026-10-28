// Base64 (RFC 4648, standard alphabet, strict decode) — the codec the
// fs.* payloads and the stream chunks ride in. Exported so the caller
// rendering stream lines encodes chunk bytes with the same table.
package agentverbs

import "strings"

const b64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var b64RevTab = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(b64Alphabet); i++ {
		t[b64Alphabet[i]] = int8(i)
	}
	return t
}()

// B64Encode renders b in the standard alphabet with tail padding.
func B64Encode(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow((len(b) + 2) / 3 * 4)
	for i := 0; i+3 <= len(b); i += 3 {
		n := uint32(b[i])<<16 | uint32(b[i+1])<<8 | uint32(b[i+2])
		sb.WriteByte(b64Alphabet[n>>18])
		sb.WriteByte(b64Alphabet[(n>>12)&63])
		sb.WriteByte(b64Alphabet[(n>>6)&63])
		sb.WriteByte(b64Alphabet[n&63])
	}
	switch rem := len(b) % 3; rem {
	case 1:
		n := uint32(b[len(b)-1]) << 16
		sb.WriteByte(b64Alphabet[n>>18])
		sb.WriteByte(b64Alphabet[(n>>12)&63])
		sb.WriteString("==")
	case 2:
		n := uint32(b[len(b)-2])<<16 | uint32(b[len(b)-1])<<8
		sb.WriteByte(b64Alphabet[n>>18])
		sb.WriteByte(b64Alphabet[(n>>12)&63])
		sb.WriteByte(b64Alphabet[(n>>6)&63])
		sb.WriteByte('=')
	}
	return sb.String()
}

// B64Decode is the strict D3 decoder: padding required, only at the
// tail, at most two '=' bytes, standard alphabet, no whitespace.
func B64Decode(s string) ([]byte, bool) {
	if len(s) == 0 {
		return nil, true
	}
	if len(s)%4 != 0 {
		return nil, false
	}
	pad := 0
	for pad < 2 && s[len(s)-1-pad] == '=' {
		pad++
	}
	body := s[:len(s)-pad]
	for i := 0; i < len(body); i++ {
		if b64RevTab[body[i]] < 0 {
			return nil, false
		}
	}
	out := make([]byte, 0, len(s)/4*3)
	for i := 0; i+4 <= len(body); i += 4 {
		n := uint32(b64RevTab[body[i]])<<18 | uint32(b64RevTab[body[i+1]])<<12 |
			uint32(b64RevTab[body[i+2]])<<6 | uint32(b64RevTab[body[i+3]])
		out = append(out, byte(n>>16), byte(n>>8), byte(n))
	}
	switch pad {
	case 0:
		if rem := len(body) % 4; rem != 0 {
			return nil, false
		}
	case 1:
		n := uint32(b64RevTab[body[len(body)-3]])<<18 |
			uint32(b64RevTab[body[len(body)-2]])<<12 |
			uint32(b64RevTab[body[len(body)-1]])<<6
		out = append(out, byte(n>>16), byte(n>>8))
	case 2:
		n := uint32(b64RevTab[body[len(body)-2]])<<18 |
			uint32(b64RevTab[body[len(body)-1]])<<12
		out = append(out, byte(n>>16))
	}
	return out, true
}
