package main

import (
	"fmt"
	"os"
	"strconv"

	"virelai/prof"
	"virelai/vi"
)

func run(args []string) error {
	seconds := 30
	var pid uint64
	var name string
	var symbols *prof.Symbols
	var err error
	if len(args) >= 2 && args[0] == "exec" {
		name = args[1]
		symbols, err = prof.LoadSymbols(name)
		if err != nil {
			return err
		}
		child, execErr := vi.Exec(name, args[2:]...)
		if execErr != nil {
			return execErr
		}
		pid = uint64(child)
	} else {
		if len(args) != 4 || args[0] != "-p" || args[2] != "-d" {
			return fmt.Errorf("usage: prof -p <pid|name> -d <secs> | prof exec <file> [args]")
		}
		seconds, err = strconv.Atoi(args[3])
		if err != nil {
			return err
		}
		target, err := prof.FindTarget(args[1])
		if err != nil {
			return err
		}
		pid, name = target.PID, target.Name()
		symbols, err = prof.LoadSymbols(name)
		if err != nil {
			return err
		}
	}
	report, err := prof.Capture(pid, seconds, symbols)
	if err != nil {
		return err
	}
	if err := prof.Save(name, report, os.Stdout); err != nil {
		return err
	}
	if report.Samples == 0 {
		return fmt.Errorf("prof: no EL0 samples")
	}
	fmt.Println("prof: done")
	return nil
}

func main() {
	if err := run(vi.Args()[1:]); err != nil {
		fmt.Println("prof: FAIL", err)
		vi.Exit(1)
	}
}
