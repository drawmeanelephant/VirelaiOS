package appkit

import (
	"testing"

	"virelai/tabapp"
	"virelai/vi"
)

func TestLoopPresentsOnceAndRepaintsDirtyEvents(t *testing.T) {
	events := []vi.Event{
		{Kind: vi.EvWinResize, Arg0: 800, Arg1: 600},
		{Kind: vi.EvMouseMove, Arg0: 3, Arg1: 4},
	}
	i := 0
	poll := func() (vi.Event, int64, bool) {
		if i == len(events) {
			return vi.Event{}, -38, false
		}
		ev := events[i]
		i++
		return ev, 1, true
	}

	draws := 0
	presents := 0
	handled := 0
	ta := &tabapp.TabApp{Win: 1, W: 512, H: 384}
	loop := NewLoop(ta, func() { draws++ }, func(vi.Event) bool {
		handled++
		return true
	})
	loop.OnInitialPresent = func() { presents++ }
	if status := loop.RunWith(poll, nil); status != 0 {
		t.Fatalf("status = %d, want normal zero on poll refusal", status)
	}
	if draws != 3 {
		t.Fatalf("draws = %d, want initial + resize + dirty = 3", draws)
	}
	if presents != 1 {
		t.Fatalf("initial-present callback = %d, want exactly one", presents)
	}
	if handled != 1 {
		t.Fatalf("handled = %d, want only non-lifecycle event = 1", handled)
	}
	if ta.W != 800 || ta.H != 600 {
		t.Fatalf("canvas = %dx%d, want 800x600", ta.W, ta.H)
	}
}

func TestLoopDoesNotRepaintAfterQuitRequest(t *testing.T) {
	poll := func() (vi.Event, int64, bool) {
		return vi.Event{Kind: vi.EvMouseDown, Arg0: 10, Arg1: 10}, 1, true
	}
	draws := 0
	exited := false
	loop := NewLoop(&tabapp.TabApp{Win: 1}, func() { draws++ }, func(vi.Event) bool { return true })
	loop.ShouldQuit = func() (int, bool) { return 7, true }
	loop.OnExit = func(status int) {
		if status != 7 {
			t.Fatalf("status = %d, want 7", status)
		}
		exited = true
	}
	if status := loop.RunWith(poll, nil); status != 7 {
		t.Fatalf("status = %d, want 7", status)
	}
	if !exited || draws != 1 {
		t.Fatalf("exited=%v draws=%d, want exit after initial draw", exited, draws)
	}
}

func TestLoopCleanExitUsesAppContract(t *testing.T) {
	poll := func() (vi.Event, int64, bool) {
		return vi.Event{Kind: vi.EvWinClose}, 1, true
	}
	exited := -1
	loop := NewLoop(&tabapp.TabApp{Win: 1}, nil, nil)
	loop.OnExit = func(status int) { exited = status }
	if status := loop.RunWith(poll, nil); status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
	if exited != 0 {
		t.Fatalf("OnExit status = %d, want 0", exited)
	}
}

func TestLoopEmptyPollYieldsWithoutDrawingAgain(t *testing.T) {
	calls := 0
	poll := func() (vi.Event, int64, bool) {
		calls++
		if calls == 1 {
			return vi.Event{}, 0, false
		}
		return vi.Event{}, -1, false
	}
	draws := 0
	slept := 0
	loop := NewLoop(nil, func() { draws++ }, nil)
	if status := loop.RunWith(poll, func(uint64) { slept++ }); status != 0 {
		t.Fatalf("status = %d, want zero on poll refusal", status)
	}
	if draws != 1 || slept != 1 {
		t.Fatalf("draws=%d slept=%d, want initial draw and one yield", draws, slept)
	}
}

func TestLoopSubscriptionRepaintsAfterReadingPublishedValue(t *testing.T) {
	oldSubscribe, oldPoll := subscribeSetting, pollSetting
	t.Cleanup(func() {
		subscribeSetting, pollSetting = oldSubscribe, oldPoll
	})
	subscribeSetting = func(key string, win uint32, name string) bool {
		return key == "theme" && win == 7 && name == "GOCALC.ELF"
	}
	polls := 0
	pollSetting = func(win uint32) (string, string, bool) {
		polls++
		if win != 7 || polls != 1 {
			return "", "", false
		}
		return "theme", "light", true
	}

	ta := &tabapp.TabApp{Win: 7, Name: "GOCALC.ELF"}
	draws := 0
	loop := NewLoop(ta, func() { draws++ }, nil)
	applied, notified := "", ""
	if !loop.SubscribeSetting("theme", func(value string) bool {
		applied = value
		return true
	}) {
		t.Fatal("theme subscription refused")
	}
	loop.OnSettingPresent = func(key, value string) {
		notified = key + "=" + value
	}
	poll := func() (vi.Event, int64, bool) {
		return vi.Event{}, -1, false
	}
	if status := loop.RunWith(poll, nil); status != 0 {
		t.Fatalf("status = %d", status)
	}
	if applied != "light" || notified != "theme=light" {
		t.Fatalf("applied=%q notified=%q", applied, notified)
	}
	if draws != 2 {
		t.Fatalf("draws=%d, want initial and changed frame", draws)
	}
}
