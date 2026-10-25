package main

// The M196d provisioning legs (issue #3433): the store-name → app-binding
// derivation, the 'p' verb's staging and its named refusals, and the
// once-per-pass verdict poll — all pure, every slot-96 call a seam var.
import (
	"strings"
	"testing"

	"virelai/vaultlib"
	"virelai/vi"
)

// stubAskSeams wires the slot-96 seams plus the receipt writer for one
// test and restores them.
func stubAskSeams(t *testing.T, ask func(string, string, string) int64,
	status func(int64) int64) (receipts *string) {
	t.Helper()
	oldAsk, oldStatus := storeAsk, storeAskStatus
	oldTime, oldAppend := timeFn, rcptAppend
	t.Cleanup(func() {
		storeAsk, storeAskStatus = oldAsk, oldStatus
		timeFn, rcptAppend = oldTime, oldAppend
	})
	storeAsk = ask
	storeAskStatus = status
	timeFn = func() int64 { return 1729440000 }
	var rows string
	receipts = &rows
	rcptAppend = func(path string, b []byte) int64 {
		rows += string(b)
		return 0
	}
	return receipts
}

func vaultApp(entries ...vaultlib.Entry) *app {
	a := &app{}
	a.m.unlocked(entries, []byte("k"), []byte("s"), testIters, 0)
	return a
}

// The mail-/irc- prefixes bind to their consumers; anything else stages
// unbound (uid-scope, the hand-edited-line shape).
func TestProvisionApps(t *testing.T) {
	for _, c := range []struct{ name, apps string }{
		{"mail-gate", "GOMAIL.ELF"},
		{"irc-gate", "GOIRC.ELF"},
		{"mail-", "GOMAIL.ELF"},
		{"bank", ""},
		{"email-x", ""},
		{"agent-sign", ""},
	} {
		if got := provisionApps(c.name); got != c.apps {
			t.Fatalf("provisionApps(%q) = %q, want %q", c.name, got, c.apps)
		}
	}
}

// 'p' stages the selected entry under its own name with the derived
// binding, receipts the ask, and queues it for the verdict poll.
func TestProvisionStagesAsk(t *testing.T) {
	var gotName, gotVal, gotApps string
	rows := stubAskSeams(t, func(name, val, apps string) int64 {
		gotName, gotVal, gotApps = name, val, apps
		return 3
	}, nil)

	a := vaultApp(vaultlib.Entry{Name: []byte("mail-gate"), Secret: []byte("pw-gate-7")})
	a.provision()

	if gotName != "mail-gate" || gotVal != "pw-gate-7" || gotApps != "GOMAIL.ELF" {
		t.Fatalf("StoreAsk(%q, %q, %q), want (mail-gate, pw-gate-7, GOMAIL.ELF)",
			gotName, gotVal, gotApps)
	}
	if len(a.asks) != 1 || a.asks[0] != (pendingAsk{id: 3, key: "mail-gate"}) {
		t.Fatalf("asks = %+v, want the staged id=3 mail-gate", a.asks)
	}
	if *rows != "1729440000 provision mail-gate\n" {
		t.Fatalf("receipts = %q, want the provision row", *rows)
	}
}

// The store bounds refuse before the ask: over-long names and secrets and
// an empty secret never reach the seam — and a full queue is a named
// refusal, not an ENOSPC surprise.
func TestProvisionRefusals(t *testing.T) {
	calls := 0
	stubAskSeams(t, func(name, val, apps string) int64 {
		calls++
		return -vi.ErrENOSPC
	}, nil)

	big := func(n int, c byte) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = c
		}
		return b
	}

	// Oversized name (store cap is 32).
	a := vaultApp(vaultlib.Entry{Name: big(33, 'n'), Secret: []byte("v")})
	a.provision()
	if calls != 0 || len(a.asks) != 0 {
		t.Fatal("a 33-byte name must refuse before the ask")
	}
	// Oversized secret (store cap is 64).
	a = vaultApp(vaultlib.Entry{Name: []byte("k"), Secret: big(65, 'v')})
	a.provision()
	if calls != 0 || len(a.asks) != 0 {
		t.Fatal("a 65-byte secret must refuse before the ask")
	}
	// Empty secret — the kernel's EINVAL, checked here so the status names it.
	a = vaultApp(vaultlib.Entry{Name: []byte("k")})
	a.provision()
	if calls != 0 || len(a.asks) != 0 {
		t.Fatal("an empty secret must refuse before the ask")
	}
	// The queue cap: four staged asks refuse the fifth.
	a = vaultApp(vaultlib.Entry{Name: []byte("mail-gate"), Secret: []byte("v")})
	for i := 0; i < askMaxPending; i++ {
		a.asks = append(a.asks, pendingAsk{id: int64(i + 1), key: "k"})
	}
	a.provision()
	if calls != 0 || len(a.asks) != askMaxPending {
		t.Fatal("a full queue must refuse a fifth provision")
	}
	// A kernel refusal is named and queues nothing.
	a = vaultApp(vaultlib.Entry{Name: []byte("mail-gate"), Secret: []byte("v")})
	a.provision()
	if calls != 1 || len(a.asks) != 0 || !strings.Contains(a.status, "ENOSPC") {
		t.Fatalf("kernel refusal: calls=%d asks=%v status=%q", calls, a.asks, a.status)
	}
}

// The verdict poll: committed and dropped are terminal (consumed on read),
// pending waits, and a vanished ask is the named refusal — never silence.
func TestPollAsksVerdicts(t *testing.T) {
	verdicts := map[int64]int64{
		1: vi.SecretAskCommitted,
		2: vi.SecretAskPending,
	}
	rows := stubAskSeams(t, nil, func(id int64) int64 { return verdicts[id] })

	a := vaultApp()
	a.asks = []pendingAsk{{id: 1, key: "mail-gate"}, {id: 2, key: "irc-x"}}
	a.pollAsks()

	if len(a.asks) != 1 || a.asks[0].key != "irc-x" {
		t.Fatalf("asks after poll = %+v, want only the pending irc-x", a.asks)
	}
	if *rows != "1729440000 provision-grant mail-gate\n" {
		t.Fatalf("receipts = %q, want the grant row", *rows)
	}

	// The drop verdict retires the last ask with its own row.
	verdicts[2] = vi.SecretAskDropped
	a.pollAsks()
	if len(a.asks) != 0 {
		t.Fatalf("asks = %+v, want drained", a.asks)
	}
	if !strings.Contains(*rows, "provision-deny irc-x") {
		t.Fatalf("receipts = %q, want the deny row", *rows)
	}

	// ENOENT — the kernel no longer knows the ask — is the named refusal
	// and still retires the row.
	verdicts[3] = -vi.ErrENOENT
	a.asks = []pendingAsk{{id: 3, key: "mail-gate"}}
	a.pollAsks()
	if len(a.asks) != 0 {
		t.Fatal("a vanished ask must retire, not poll forever")
	}
	if !strings.Contains(a.status, "mail-gate") {
		t.Fatalf("status = %q, want the named loss", a.status)
	}
}
