package main

import (
	"virelai/observe"
	"virelai/vi"
)

func main() {
	if err := observe.Run(vi.Args()); err != nil {
		vi.ConsoleLine(err.Error())
		vi.Exit(1)
	}
}
