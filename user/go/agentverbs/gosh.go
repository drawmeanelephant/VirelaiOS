// gosh.run: spawn the shell image with the command redirected into the
// audit-lane capture file, wait it out inside the caller's budget, then
// stream the capture back in bounded chunks (ADR 0044 D3, M109b).
package agentverbs

import (
	"strings"

	"virelai/shlib"
	"virelai/vi"
)

// GoshRun answers gosh.run. The daemon's exec flag is the operator's
// kill switch and refuses verb_disabled before any argument check; the
// command grammar is the shlib agent line; the capture file is deleted
// before and after the run so a stale capture never leaks across runs.
func (d *Dispatcher) GoshRun(req *Request, s *Session) (string, string) {
	if !s.AllowExec {
		return "", "verb_disabled"
	}
	cmd, present, ok := req.argStr("cmd")
	if !present || !ok {
		return "", "bad_request"
	}
	if len(cmd) == 0 || len(cmd) > CmdMax {
		return "", "too_large"
	}
	for i := 0; i < len(cmd); i++ {
		if cmd[i] < 0x20 || cmd[i] > 0x7e {
			return "", "cmd_rejected"
		}
	}
	if !shlib.CheckAgentLine(cmd) {
		return "", "cmd_rejected"
	}
	ms, present, ok := req.argInt("timeout_ms", 1, TimeoutMaxMs)
	if present && !ok {
		return "", "bad_request"
	}
	if !present {
		ms = timeoutDefMs
	}
	_ = d.cfg.Seams.DeleteFile(d.cfg.OutPath)
	// D4 residual (a): the attempt is on record BEFORE the child starts —
	// a term-capped agent can reach the audit lane through a shell line,
	// so this intent receipt is the untouchable-in-order part of the
	// trail. The post-dispatch receipt still carries the real result
	// (ok/timed_out/spawn_failed); the pair is one dispatch's record,
	// not a double count. AGENTD wires AuditIntent to its audit lane.
	if s.AuditIntent != nil {
		s.AuditIntent()
	}
	// The task pool is bounded; a just-exited child's slots stay zombies
	// until the idle task reaps them, so a back-to-back spawn can land
	// ENOSPC. That refusal is transient — yield and retry a bounded
	// window before reporting spawn_failed (real refusals fail on the
	// first try).
	pid, err := d.cfg.Seams.Exec(d.cfg.GoshBin, "-c", cmd+" > "+d.cfg.OutPath)
	for spinNs := d.cfg.Seams.Nanos(); err != nil && err.Error() == "ENOSPC"; {
		if d.cfg.Seams.Nanos()-spinNs >= spawnRetryNs {
			break
		}
		d.cfg.Seams.Yield()
		pid, err = d.cfg.Seams.Exec(d.cfg.GoshBin, "-c", cmd+" > "+d.cfg.OutPath)
	}
	if err != nil {
		return "", "spawn_failed"
	}
	d.log(d.cfg.Mark + ": spawn pid=" + vi.Itoa64(pid))
	st, werr := d.cfg.Seams.Wait(pid, ms*int64(1e6))
	if werr != nil {
		_ = d.cfg.Seams.Kill(uint64(pid))
		st = d.reapConfirm(pid)
		d.log(d.cfg.Mark + ": reap pid=" + vi.Itoa64(pid) + " status=" + vi.Itoa64(st))
		_ = d.cfg.Seams.DeleteFile(d.cfg.OutPath) // the capture file is per-run; a killed child still leaves one
		return "", "timed_out"
	}
	d.log(d.cfg.Mark + ": reap pid=" + vi.Itoa64(pid) + " status=" + vi.Itoa64(st))
	// The child's task slots free only when the idle task reaps them —
	// Probe going Absent (descriptor recycled) is the observable edge of
	// that. Settling here keeps the NEXT back-to-back gosh.run from
	// tripping a full pool mid-start.
	d.settleChild(pid)
	out, r := d.cfg.Seams.ReadFile(d.cfg.OutPath, OutMax+1)
	if r < 0 {
		out = nil // no capture file: the command printed nothing
	}
	truncated := len(out) > OutMax
	if truncated {
		out = out[:OutMax]
	}
	_ = d.cfg.Seams.DeleteFile(d.cfg.OutPath)
	// Bounded streaming (M109b): the captured bytes ride stream chunk
	// lines ahead of the result — the line bound never moves and the
	// client reassembles seq order. GOSH's > redirect materializes the
	// file only when the child exits, so chunking-after-reap is the
	// honest streaming shape this pipe can offer.
	streamed := len(out)
	emitChunks(s, out)
	var sb strings.Builder
	sb.WriteString(`{"exit_status":`)
	sb.WriteString(vi.Itoa64(st))
	sb.WriteString(`,"stdout_bytes":`)
	sb.WriteString(vi.Itoa64(int64(streamed)))
	sb.WriteString(`,"truncated":`)
	sb.WriteString(jbool(truncated))
	sb.WriteByte('}')
	return sb.String(), ""
}

// settleChild yield-polls Probe until the child's descriptor recycles
// (ProbeAbsent) or the bound lapses — best-effort, never fatal.
func (d *Dispatcher) settleChild(pid int64) {
	for start := d.cfg.Seams.Nanos(); d.cfg.Seams.Nanos()-start < spawnRetryNs; {
		if _, state := d.cfg.Seams.Probe(pid); state == vi.ProbeAbsent {
			return
		}
		d.cfg.Seams.Yield()
	}
}

// reapConfirm polls Probe after a Kill until the child leaves the
// running state, returning the last observed status (-1 when it simply
// vanished — the reaped-and-recycled case).
func (d *Dispatcher) reapConfirm(pid int64) int64 {
	st := int64(-1)
	for i := 0; i < reapProbeTries; i++ {
		s, state := d.cfg.Seams.Probe(pid)
		switch state {
		case vi.ProbeExited:
			return s
		case vi.ProbeAbsent:
			return st // reaped and recycled: -1 is the honest answer
		default:
			st = s
			d.cfg.Seams.Sleep(1)
		}
	}
	return st
}
