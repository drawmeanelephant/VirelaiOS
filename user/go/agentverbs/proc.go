// The proc verbs: bounded reads of the kernel proc table rendered in
// the shared proc.list/proc.get JSON shape (ADR 0044 D3).
package agentverbs

import (
	"strings"

	"virelai/vi"
)

// stateName maps a ProcRow state onto the D3 enum. The kernel only
// emits 1/2/3; anything else cannot be represented and is reported as
// an io_error by the caller rather than silently mislabelled.
func stateName(s uint64) (string, bool) {
	switch s {
	case vi.ProcCreated:
		return "created", true
	case vi.ProcRunning:
		return "running", true
	case vi.ProcExited:
		return "exited", true
	}
	return "", false
}

// procRowJSON appends one row in the shared proc.list/proc.get shape:
// exit_status is null unless the state is exited.
func procRowJSON(sb *strings.Builder, r *vi.ProcRow) string {
	state, ok := stateName(r.State)
	if !ok {
		return "io_error"
	}
	sb.WriteString(`{"pid":`)
	sb.WriteString(vi.Itoa64(int64(r.PID)))
	sb.WriteString(`,"name":`)
	jwriteStr(sb, r.Name())
	sb.WriteString(`,"state":`)
	jwriteStr(sb, state)
	sb.WriteString(`,"exit_status":`)
	if r.State == vi.ProcExited {
		sb.WriteString(vi.Itoa64(int64(r.ExitStatus)))
	} else {
		sb.WriteString("null")
	}
	sb.WriteByte('}')
	return ""
}

// ProcList answers proc.list: up to maxProcRows rows in kernel order.
func (d *Dispatcher) ProcList() (string, string) {
	rows := make([]vi.ProcRow, maxProcRows)
	n, r := d.cfg.Seams.Procs(rows)
	if r < 0 {
		return ioErr(r)
	}
	var sb strings.Builder
	sb.WriteString(`{"procs":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		if code := procRowJSON(&sb, &rows[i]); code != "" {
			return "", code
		}
	}
	sb.WriteString(`]}`)
	return sb.String(), ""
}

// ProcGet answers proc.get: the one row matching pid, else not_found.
func (d *Dispatcher) ProcGet(req *Request) (string, string) {
	pid, present, ok := req.argInt("pid", 1, intMax)
	if !present || !ok {
		return "", "bad_request"
	}
	rows := make([]vi.ProcRow, maxProcRows)
	n, r := d.cfg.Seams.Procs(rows)
	if r < 0 {
		return ioErr(r)
	}
	for i := 0; i < n; i++ {
		if int64(rows[i].PID) == pid {
			var sb strings.Builder
			if code := procRowJSON(&sb, &rows[i]); code != "" {
				return "", code
			}
			return sb.String(), ""
		}
	}
	return "", "not_found"
}
