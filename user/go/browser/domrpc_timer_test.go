package main

import "testing"

// #3913: the app timer is worker-owned. A scriptless page cancels and arms
// nothing; a live worker disarms its own empty table, arms its earliest
// deadline, and retires the arm at teardown.
func TestScriptTimerTeardownOnlyTouchesOwnedState(t *testing.T) {
	prevCancel, prevSet := timerCancel, timerSetNs
	defer func() { timerCancel, timerSetNs = prevCancel, prevSet }()
	cancels, arms := 0, 0
	var armedNs uint64
	timerCancel = func() int64 { cancels++; return 0 }
	timerSetNs = func(ns uint64) int64 { arms++; armedNs = ns; return 0 }

	a := &app{}
	a.closeScripts()
	a.armScriptTimer()
	if cancels != 0 || arms != 0 {
		t.Fatalf("scriptless page touched the app timer: cancel=%d arm=%d", cancels, arms)
	}

	a.rpc = &scriptWorker{pid: -1}
	a.armScriptTimer() // owned, but no granted timer: disarm
	if cancels != 1 || arms != 0 {
		t.Fatalf("empty worker timer table: cancel=%d arm=%d, want 1/0", cancels, arms)
	}

	a.rpc.timers = []scriptTimer{{id: 7, due: 1 << 62}}
	a.armScriptTimer()
	if arms != 1 || armedNs < 1_000_000 {
		t.Fatalf("worker arm: arm=%d ns=%d, want one arm >= 1ms", arms, armedNs)
	}
	if cancels != 1 {
		t.Fatalf("arming cancelled an owned timer: cancel=%d", cancels)
	}

	a.closeScripts()
	if a.rpc != nil {
		t.Fatal("closeScripts left the worker attached")
	}
	if cancels != 2 {
		t.Fatalf("worker teardown cancels=%d, want 2 (owned disarm + retire)", cancels)
	}
}
