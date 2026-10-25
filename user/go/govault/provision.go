// provision.go — M196d (issue #3433): the vault's slot-96 provisioning
// verb. An unlocked vault entry can be asked into the kernel secret store
// under its own name — the consent-gated `sys_secret_set` ask path M196a
// landed — so a mail-<acct>/irc-<acct> row is provisioned through the
// seat's modal instead of a hand-edited SECRETS.TXT. The asker side is
// honest bookkeeping: the verb stages, the seat's modal decides, and the
// loop's once-per-pass status read reports the verdict the kernel
// recorded — grant, deny, or a named refusal. Receipts mirror that arc so
// the audit file carries the ask AND its answer, never just the request.
//
// The value crosses only kernel memory — the staged line lives in the
// pending-ask table until the gesture lands it, and the marker vocabulary
// plus the receipt rows carry the entry NAME only, the same discipline
// the rest of the markers keep.
package main

import (
	"strings"

	"virelai/vi"
)

// pendingAsk is one staged slot-96 ask awaiting the seat's gesture. It is
// bookkeeping — the kernel's ask id plus the store key it names, both
// loggable — never session material, so it survives lock() and the poll
// still reports its verdict.
type pendingAsk struct {
	id  int64
	key string
}

// askMaxPending mirrors the kernel's max_pending_asks (secret.zig): the
// consent queue is small by design — a human answers each row — so the
// fifth 'p' is refused here before spending an ENOSPC round-trip.
const askMaxPending = 4

// provisionApps is the app-binding the staged row asks for — the consumer
// the row exists FOR, derived from the M196b/c store-name convention:
// mail-<acct> serves GOMAIL, irc-<acct> serves GOIRC. Any other name
// stages unbound, the same uid-scope a hand-edited SECRETS.TXT line
// without the fourth field carries. The kernel refuses a list that names
// the COMMITTER, never the asker — GOVAULT staging another app's row is
// exactly the intended use.
func provisionApps(name string) string {
	switch {
	case strings.HasPrefix(name, "mail-"):
		return "GOMAIL.ELF"
	case strings.HasPrefix(name, "irc-"):
		return "GOIRC.ELF"
	}
	return ""
}

// provision is the list face's 'p': stage the selected entry into the
// kernel's pending-ask table under its own name. The store bounds are
// checked up front so the status line names the real problem (a 48-char
// vault name or a 256-byte secret can never fit a 32/64 store row) rather
// than echoing the kernel's EINVAL.
func (a *app) provision() {
	if a.m.sel < 0 || a.m.sel >= len(a.m.entries) {
		return
	}
	e := &a.m.entries[a.m.sel]
	name := string(e.Name)
	if len(e.Name) == 0 || len(e.Name) > vi.SecretKeyMax ||
		len(e.Secret) == 0 || len(e.Secret) > vi.SecretValMax {
		vi.ConsoleLine(markerProvRef + name + " bounds")
		a.status = "provision: name ≤32, secret 1–64"
		return
	}
	if len(a.asks) >= askMaxPending {
		vi.ConsoleLine(markerProvRef + name + " queue")
		a.status = "provision queue full — answer the seat prompts"
		return
	}
	id := storeAsk(name, string(e.Secret), provisionApps(name))
	if id < 0 {
		vi.ConsoleLine(markerProvRef + name + " " + vi.ErrnoName(id))
		a.status = "provision refused: " + vi.ErrnoName(id)
		return
	}
	a.asks = append(a.asks, pendingAsk{id: id, key: name})
	vi.ConsoleLine(markerProvision + name)
	a.receipt(actProvision, []byte(name))
	a.status = "asked to provision " + name
}

// pollAsks reads each staged ask's verdict once per loop pass — the same
// site the state-ask poll keeps, so the answer lands even while the seat's
// modal owns the keyboard. A terminal verdict is consumed on read (#3406):
// mark it, receipt it, retire it. Pending simply waits; a vanished ask
// (ENOENT — the kernel reaped the row) or an unverdict-like result is the
// named refusal, never silence. Runs on every face: a locked vault still
// owes the store its answer.
func (a *app) pollAsks() {
	if len(a.asks) == 0 {
		return
	}
	kept := a.asks[:0]
	for _, p := range a.asks {
		switch st := storeAskStatus(p.id); st {
		case vi.SecretAskPending:
			kept = append(kept, p)
		case vi.SecretAskCommitted:
			vi.ConsoleLine(markerProvGrant + p.key)
			a.receipt(actProvGrant, []byte(p.key))
			a.status = "provisioned " + p.key
		case vi.SecretAskDropped:
			vi.ConsoleLine(markerProvDeny + p.key)
			a.receipt(actProvDeny, []byte(p.key))
			a.status = "provision denied " + p.key
		default:
			if st < 0 {
				vi.ConsoleLine(markerProvRef + p.key + " " + vi.ErrnoName(st))
			} else {
				vi.ConsoleLine(markerProvRef + p.key + " verdict=" + vi.Itoa64(st))
			}
			a.status = "provision verdict lost " + p.key
		}
	}
	a.asks = kept
}
