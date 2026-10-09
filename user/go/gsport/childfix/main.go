// childfix is the M95e child fixture: one ELF whose argv selects its
// behaviour, built by tools/go/build-gschild.sh and staged under several
// process names so a gate's name-based kill always reaches the right
// incarnation.
//
//	childfix [log <label> <n>] <mode> [args]
//
// The optional `log` prefix writes n bounded lines into the caller's
// APPLOG ring (the cooperating-child capture contract — gsport/child's
// AppendLog is the only writer besides vi.Log) before running the mode:
//
//	exit <n>   clean exit with status n
//	panic      Go panic -> the runtime exits 2
//	fault      a BRK instruction -> unhandled, kernel reports 139
//	sleep      idle until killed (the external-kill case)
//
// The package lives under gsport, so the guard keeps virelai/vi out of
// it: markers ride fmt (stdout is the console), log lines ride
// child.AppendLog, and there is deliberately no signal, exec or wait use.
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"virelai/gsport/child"
)

// faultBrk executes a BRK — see fault_arm64.s for why that is the only
// honest unhandled-fault a guest program can produce.
func faultBrk()

func fail(msg string) {
	fmt.Println("childfix: FAIL " + msg)
	os.Exit(70)
}

func main() {
	args := os.Args[1:]

	// `log <label> <n>` prefixes: bounded ring writes, then the mode.
	for len(args) >= 3 && args[0] == "log" {
		label := args[1]
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 1 || n > 64 {
			fail("bad log count " + args[2])
		}
		for i := 0; i < n; i++ {
			if err := child.AppendLog(label, fmt.Sprintf("childfix %s line %d", label, i)); err != nil {
				fail("applog " + err.Error())
			}
		}
		args = args[3:]
	}

	mode := "sleep"
	if len(args) > 0 {
		mode = args[0]
	}
	fmt.Println("childfix: mode=" + mode)
	switch mode {
	case "exit":
		code := 0
		if len(args) > 1 {
			if v, err := strconv.Atoi(args[1]); err == nil {
				code = v
			}
		}
		os.Exit(code)
	case "panic":
		panic("childfix panic")
	case "fault":
		faultBrk()
	case "sleep":
		for {
			time.Sleep(time.Hour)
		}
	default:
		fail("unknown mode " + mode)
	}
}
