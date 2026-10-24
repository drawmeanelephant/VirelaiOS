package main

import (
	"testing"

	"virelai/vi"
)

// stubSecrets wires the slot-96 seams for one test and restores them.
func stubSecrets(t *testing.T, list func(uint32, []byte) (int, int64), commit, drop func(uint32) int64) {
	t.Helper()
	l, c, d := secretAskList, secretAskCommit, secretAskDrop
	secretAskList, secretAskCommit, secretAskDrop = list, commit, drop
	t.Cleanup(func() {
		secretAskList, secretAskCommit, secretAskDrop = l, c, d
		secretAskModal = secretAsk{}
	})
}

func TestSecretAskLifecycleGrantAndDecline(t *testing.T) {
	var commitID, dropID uint32
	var log []string
	oldLog := secretLog
	secretLog = func(s string) { log = append(log, s) }
	t.Cleanup(func() { secretLog = oldLog })
	oldOpen, oldQuery, oldClose, oldRaise := openInputSink, queryWindow, closeWin, focusRaise
	openInputSink = func(x, y, w, h uint32) (int, int64) { return 91, 0 }
	queryWindow = func(id int) ([8]uint32, int64) { var st [8]uint32; return st, 0 }
	closeWin = func(id uint32) int64 { return 0 }
	focusRaise = func(id uint32) int64 { return 0 }
	t.Cleanup(func() { openInputSink, queryWindow, closeWin, focusRaise = oldOpen, oldQuery, oldClose, oldRaise })

	pending := []string{"7\t41\t1000\tGOMAIL.ELF\tmail-x\tGOMAIL.ELF"}
	stubSecrets(t,
		func(i uint32, buf []byte) (int, int64) {
			if int(i) >= len(pending) {
				return 0, -vi.ErrENOENT
			}
			copy(buf, pending[i])
			return len(pending[i]), int64(len(pending[i]))
		},
		func(id uint32) int64 { commitID = id; return 0 },
		func(id uint32) int64 { dropID = id; return 0 })

	pollSecretAsks()
	if !secretAskModal.open || secretAskModal.id != 7 {
		t.Fatalf("modal open=%v id=%d", secretAskModal.open, secretAskModal.id)
	}
	if log[len(log)-1] != MarkerSecretAsk+"GOMAIL.ELF key=mail-x" {
		t.Fatalf("ask marker: %v", log)
	}
	// An unrelated key is swallowed, not answered.
	if !handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x1e, Arg1: 'x'}) || commitID != 0 {
		t.Fatalf("stray key committed %d", commitID)
	}
	// 'g' commits the staged ask and closes the prompt.
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x22, Arg1: 'g'})
	if commitID != 7 || secretAskModal.open {
		t.Fatalf("commit id=%d open=%v", commitID, secretAskModal.open)
	}
	if log[len(log)-1] != MarkerSecretGrant+"GOMAIL.ELF key=mail-x" {
		t.Fatalf("grant marker: %v", log)
	}

	// A second ask stands after the first answered; Esc declines it.
	pending = []string{"9\t42\t1000\tGOIRC.ELF\tirc"}
	pollSecretAsks()
	if !secretAskModal.open || secretAskModal.id != 9 {
		t.Fatalf("second ask open=%v", secretAskModal.open)
	}
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x29})
	if dropID != 9 || secretAskModal.open {
		t.Fatalf("drop id=%d open=%v", dropID, secretAskModal.open)
	}
	if log[len(log)-1] != MarkerSecretDecline+"GOIRC.ELF key=irc" {
		t.Fatalf("decline marker: %v", log)
	}
}

func TestSecretAskParseAndRefusalMarker(t *testing.T) {
	if _, ok := parseSecretAskLine("1\t41\t1000\tA.ELF\tk\tAPPS"); !ok {
		t.Fatal("valid line refused")
	}
	for _, bad := range []string{"", "1\t2\t3", "x\t41\t1000\tA\tk", "1\t41\tx\tA\tk", "1\t41\t1000\t\tk"} {
		if _, ok := parseSecretAskLine(bad); ok {
			t.Fatalf("malformed line accepted: %q", bad)
		}
	}
	var log []string
	oldLog := secretLog
	secretLog = func(s string) { log = append(log, s) }
	t.Cleanup(func() { secretLog = oldLog })
	oldOpen, oldQuery := openInputSink, queryWindow
	openInputSink = func(x, y, w, h uint32) (int, int64) { return 91, 0 }
	queryWindow = func(id int) ([8]uint32, int64) { var st [8]uint32; st[5] = 1; return st, 0 }
	t.Cleanup(func() { openInputSink, queryWindow = oldOpen, oldQuery })
	stubSecrets(t,
		func(i uint32, buf []byte) (int, int64) {
			if i != 0 {
				return 0, -vi.ErrENOENT
			}
			l := "5\t41\t1000\tAPP.ELF\tk"
			copy(buf, l)
			return len(l), int64(len(l))
		},
		func(id uint32) int64 { return -vi.ErrENOSPC },
		func(id uint32) int64 { return 0 })
	pollSecretAsks()
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x22, Arg1: 'g'})
	if log[len(log)-1] != MarkerSecretRefuse+"APP.ELF key=k ENOSPC" {
		t.Fatalf("refuse marker: %v", log)
	}
}
