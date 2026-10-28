// The fs verbs: bounded reads and writes inside the dispatcher's
// sandbox root (ADR 0044 D3). The root arrives as Config.Sandbox —
// AGENTD passes /host/AGENT; a second caller passes its own data root.
package agentverbs

import (
	"strings"

	"virelai/vi"
)

// FSRead answers fs.read: a whole file under the sandbox, base64'd,
// bounded at ReadMax bytes.
func (d *Dispatcher) FSRead(req *Request) (string, string) {
	path, present, ok := req.argStr("path")
	if !present || !ok {
		return "", "bad_request"
	}
	if !d.validPath(path) {
		return "", "path_rejected"
	}
	b, r := d.cfg.Seams.ReadFile(d.prefix+path, ReadMax+1)
	if r < 0 {
		return d.fileErr(r, path)
	}
	if len(b) > ReadMax {
		return "", "too_large"
	}
	var sb strings.Builder
	sb.WriteString(`{"path":`)
	jwriteStr(&sb, path)
	sb.WriteString(`,"size":`)
	sb.WriteString(vi.Itoa64(int64(len(b))))
	sb.WriteString(`,"data_b64":`)
	jwriteStr(&sb, B64Encode(b))
	sb.WriteByte('}')
	return sb.String(), ""
}

// FSWrite answers fs.write: a strict-base64 body written through the
// kernel's safe-write path under the sandbox, bounded at ReadMax
// decoded bytes.
func (d *Dispatcher) FSWrite(req *Request) (string, string) {
	path, present, ok := req.argStr("path")
	if !present || !ok {
		return "", "bad_request"
	}
	body, present, ok := req.argStr("data_b64")
	if !present || !ok {
		return "", "bad_request"
	}
	if !d.validPath(path) {
		return "", "path_rejected"
	}
	data, ok := B64Decode(body)
	if !ok {
		return "", "bad_request"
	}
	if len(data) > ReadMax {
		return "", "too_large"
	}
	if d.isDir(path) {
		return "", "not_a_file"
	}
	if r := d.cfg.Seams.WriteFile(d.prefix+path, data); r < 0 {
		return d.fileErr(r, path)
	}
	var sb strings.Builder
	sb.WriteString(`{"path":`)
	jwriteStr(&sb, path)
	sb.WriteString(`,"size":`)
	sb.WriteString(vi.Itoa64(int64(len(data))))
	sb.WriteByte('}')
	return sb.String(), ""
}
