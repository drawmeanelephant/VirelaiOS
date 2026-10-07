package main

import (
	"virelai/vi"
)

func main() {
	defer vi.CrashGuard("CRASHFIX.ELF")
	token := "first"
	if args := vi.Args(); len(args) > 1 {
		token = args[1]
	}
	vi.ConsoleLine("crashfix: t0=" + vi.Itoa64(vi.Nanos()) + " token=" + token)
	first(token)
}

// Keep distinct real frames even when the host linker optimizes this fixture.
//
//go:noinline
func first(token string) { second(token) }

//go:noinline
func second(token string) { third(token) }

//go:noinline
func third(token string) { panic(token) }
