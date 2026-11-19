package main

import (
	"bytes"

	"virelai/flintstore"
	"virelai/shlib"
	"virelai/sshlib"
)

// authDomain is the HMAC preimage domain of docs/flint-server-spec.md §3.2
// and the host `flintd` oracle: HMAC-SHA256(key, domain || nonce[16]).
const authDomain = "FLINT-SYNC-AUTH/1\x00"

// maxAuthKeyFileBytes bounds the key file read; a key file is 65 bytes.
const maxAuthKeyFileBytes = 4096

// parseAuthKey decodes a 64-lowercase-hex-character key (with an optional
// trailing CR/LF) — the shared-secret format §3.2 names.
func parseAuthKey(raw []byte) ([32]byte, bool) {
	var key [32]byte
	trimmed := bytes.TrimRight(raw, "\r\n")
	decoded, ok := flintstore.ParseHex32Lower(string(trimmed))
	if !ok {
		return key, false
	}
	return decoded, true
}

// readDetail names why a secret/allowlist read failed (the FS error's path
// and errno), so the serial tells EACCES from ENOENT instead of only
// `secret_malformed`. It never carries file content.
func readDetail(err error) string {
	if fe, ok := err.(*flintstore.FSError); ok {
		return fe.Error()
	}
	return ""
}

// loadAuthKeyFile reads --auth-key. The file lives outside the store root so
// the store stays byte-portable with no secrets inside it.
func loadAuthKeyFile(fs flintstore.FS, path string) (*[32]byte, error) {
	raw, err := fs.ReadFile(path, maxAuthKeyFileBytes)
	if err != nil {
		return nil, flintstore.Refuse("secret_malformed", readDetail(err))
	}
	key, ok := parseAuthKey(raw)
	if !ok {
		return nil, flintstore.Refuse("secret_malformed", "invalid --auth-key file")
	}
	return &key, nil
}

// parseAllowAuthor decodes one lower-hex Ed25519 public key per line,
// ignoring blank lines and `#` comments (§3.2).
func parseAllowAuthor(raw []byte) (map[string]bool, error) {
	authors := map[string]bool{}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.Trim(line, " \t\r")
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if _, ok := flintstore.ParseHex32Lower(string(line)); !ok {
			return nil, flintstore.Refuse("secret_malformed", "invalid --allow-author file")
		}
		authors[string(line)] = true
	}
	return authors, nil
}

// loadAllowAuthorFile reads --allow-author. The map is empty (but non-nil)
// when the file lists no keys, which still gates every append.
func loadAllowAuthorFile(fs flintstore.FS, path string) (map[string]bool, error) {
	raw, err := fs.ReadFile(path, flintstore.MaxFileReadBytes)
	if err != nil {
		return nil, flintstore.Refuse("secret_malformed", readDetail(err))
	}
	return parseAllowAuthor(raw)
}

// computeAuthHMAC answers a hello challenge exactly as the client's
// computeAuthHMAC does: hex(HMAC-SHA256(key, "FLINT-SYNC-AUTH/1\x00" || nonce)).
func computeAuthHMAC(key *[32]byte, nonce [16]byte) string {
	msg := make([]byte, 0, len(authDomain)+len(nonce))
	msg = append(msg, authDomain...)
	msg = append(msg, nonce[:]...)
	mac := shlib.HMACSHA256(key[:], msg)
	// Wipe the derived message: it is a MAC preimage, not store content.
	sshlib.Wipe(msg)
	defer sshlib.Wipe(mac[:])
	return sshlib.EncodeHex(mac[:])
}

// encodeHex spells bytes as lowercase hex (the nonce reply's shape).
func encodeHex(b []byte) string { return sshlib.EncodeHex(b) }

// hmacHexEqual compares two lowercase-hex HMAC spellings without an early
// exit on the first differing byte.
func hmacHexEqual(got, want string) bool {
	if len(got) != len(want) {
		return false
	}
	var diff byte
	for i := 0; i < len(got); i++ {
		diff |= got[i] ^ want[i]
	}
	return diff == 0
}
