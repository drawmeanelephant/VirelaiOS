// Command flint is FLINT.ELF — the M103 (FLINT M0) source-control CLI
// (ADR 0049). M103a ships exactly one verb: `parse-corpus <dir>`, the
// in-guest proof that go/parser and the Go-native Zig scanner run under
// GOOS=virelai and produce the §3 Atoms the rest of the chain consumes.
// Store, verbs and merge land in M103b-d (#2251/#2252/#2254).
package main

import (
	"virelai/vi"
)

// argvEnvpGuard pads the writable segment's bss so its end keeps at least
// 0x908 bytes of page slack: the kernel packs argv+envp into the tail of
// the writable segment and protects that block against the runtime's
// break, so the break must start past it or the runtime's first mmap is
// refused. build-flint.sh asserts the resulting slack from the ELF.
var argvEnvpGuard [0x1800]byte

func main() {
	argvEnvpGuard[0] = 1

	args := vi.Args()
	if len(args) == 3 && args[1] == "parse-corpus" {
		parseCorpus(args[2])
		return
	}
	vi.ConsoleLine("flint: usage: FLINT.ELF parse-corpus <dir>")
	vi.Exit(2)
}
