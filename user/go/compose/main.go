// Command compose is the M14 S3 composition probe (claim 3289), rehomed here by
// #1485 when the Zig NOTEPAD.BIN that proved it was deleted.
//
// It is a purpose-built FIXTURE, not a product app, and that is deliberate: the
// card that deleted the Zig app sanctioned "a Go app or a purpose-built fixture"
// for every spec that still needed one, and the composition's subject is the two
// shared user services, not any one app's editing surface:
//
//   - S1 (claim 0169, slots 38/39). Read the ONE shared kernel clipboard without
//     consuming it, then publish the same bytes back — both directions in one EL0
//     session. The gate pre-loads the clipboard with the terminal's `clip`
//     command, which is a KERNEL MONITOR command rather than a process syscall,
//     so the boot's counter reads `38 sys_clipboard_set calls=1` (this fixture's
//     copy) and `39 sys_clipboard_get calls=1` (its paste).
//   - S2 (claim 7323, slot 40). Arm the caller's ONE app timer, then BLOCK in
//     poll/sleep instead of spinning, and re-arm on every TIMER event (kind 9)
//     until the toggle count is reached. That re-arm is why the gate's
//     `40 sys_timer_set calls=7` is one arm plus one per toggle — pinned by a
//     host test, not by re-reading this comment.
//
// Why not GOEDIT.ELF, which took the other M66c rehomes: every referenced string
// literal costs 16 bytes of the image's DATA segment, and the Go runtime's sbrk
// heap needs the argv/envp block (256 + 2048 bytes, kernel/src/exec.zig) to fit
// below `memRound(firstmoduledata.end)`. Above `mem_size mod 4096 = 1792` that
// block straddles the break start, the runtime's FIRST sys_mmap is refused, and
// the app dies with `fatal error: runtime: cannot allocate memory` inside
// mallocinit. GOEDIT.ELF had 240 bytes of that budget and this probe's markers
// need ~192, so the probe lives where the budget is: a fixture with no window, no
// argv and no editing surface, whose own data segment sits ~1 KB under the wall.
//
// It takes NO arguments on purpose: with argc == 0 the kernel packs no argv/envp
// block at all, so this fixture cannot be pushed over the wall by its own markers.
//
// Build (the shared generic builder — this app has no tools/go wrapper yet
// because that directory was held by an active claim when it landed):
//
//	GO_BUILD_NAME=GOCOMP bash tools/go/build-go.sh user/go/compose/main.go
//
// Every marker below is printed only AFTER its syscall returned, so a gate
// asserting them can only pass if the calls actually landed.
package main

import (
	"virelai/vi"
	"virelai/vsys"
)

const (
	// The marker lines the class-B gate greps.
	markerPasted  = "compose: pasted n="
	markerCopied  = "compose: copied n="
	markerArmed   = "compose: armed blink"
	markerBlink   = "compose: blink"
	markerDone    = "compose: done"
	markerExiting = "compose: exiting "
	// Failure rows: each names the service it failed on and carries the kernel's
	// raw result, so a red boot says WHERE it failed instead of only that it did.
	// M97g (#2082): a negative rc is the CAP_CLIPBOARD refusal — the gate runs
	// one unprivileged exec to prove it, so the marker is distinct from empty.
	markerPasteRefused = "compose: paste refused rc="
	markerCopyRefused  = "compose: copy refused rc="
	markerPasteFail    = "compose: paste failed (clipboard empty)"
	markerCopyFail     = "compose: copy failed rc="
	markerArmFail      = "compose: timer arm failed rc="
)

// The M14 S3 cadence, the Zig probe's values verbatim: one toggle per
// blinkIntervalTicks SCHEDULER ticks (the sys_sleep clock — 1 s on VZ), and
// blinkToggles toggles before the composition completes.
const (
	blinkIntervalTicks = 8
	blinkToggles       = 6
)

// Exit statuses. exitOK is 43, the Zig selfdemo's own completion status, kept so
// the gate's `... exited status=43` asserts keep the meaning they had.
const (
	exitOK        = 43
	exitPasteFail = 2
	exitCopyFail  = 4
	exitArmFail   = 5
	exitPollFail  = 6
)

func main() {
	// S1 read path: the clipboard body the gate pre-loaded with `clip`.
	body, rc := vsys.ClipboardGet(vsys.ClipboardMax)
	if rc < 0 {
		vi.ConsoleLine(markerPasteRefused + vi.Itoa64(rc))
		vi.Exit(exitPasteFail)
	}
	if len(body) == 0 {
		vi.ConsoleLine(markerPasteFail)
		vi.Exit(exitPasteFail)
	}
	vi.ConsoleLine(markerPasted + vi.Itoa64(int64(len(body))))

	// S1 write path: publish exactly those bytes back through slot 38.
	n, rc := vsys.ClipboardSet(body)
	if rc < 0 {
		vi.ConsoleLine(markerCopyRefused + vi.Itoa64(rc))
		vi.Exit(exitCopyFail)
	}
	vi.ConsoleLine(markerCopied + vi.Itoa64(int64(n)))

	// S2: arm the caller's ONE app timer, then go back to the event loop.
	if rc := vsys.TimerSet(blinkIntervalTicks); rc < 0 {
		vi.ConsoleLine(markerArmFail + vi.Itoa64(rc))
		vi.Exit(exitArmFail)
	}
	vi.ConsoleLine(markerArmed)

	// S2 continued: each TIMER event toggles and re-arms, so this session makes
	// 1 + blinkToggles sys_timer_set calls in total.
	toggles := 0
	for {
		ev, r, ok := vi.PollEventRaw()
		if !ok {
			if r < 0 {
				vi.Exit(exitPollFail)
			}
			vi.Sleep(1)
			continue
		}
		if ev.Kind != vi.EvTimer {
			continue
		}
		toggles++
		vi.ConsoleLine(markerBlink)
		if rc := vsys.TimerSet(blinkIntervalTicks); rc < 0 {
			vi.ConsoleLine(markerArmFail + vi.Itoa64(rc))
			vi.Exit(exitArmFail)
		}
		if toggles >= blinkToggles {
			vi.ConsoleLine(markerDone)
			vi.ConsoleLine(markerExiting + vi.Itoa64(exitOK))
			vi.Exit(exitOK)
		}
	}
}
