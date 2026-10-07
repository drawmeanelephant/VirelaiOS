package main

import (
	"virelai/heap"
	"virelai/vi"
)

func main() {
	if err := heap.Run(vi.Args()); err != nil {
		vi.ConsoleLine(err.Error())
		vi.Exit(1)
	}
}
