//go:build virelai

package main

import (
	"virelai/pdfproof"
	"virelai/vi"
)

func main() {
	if code := pdfproof.Run(render); code != 0 {
		vi.Console("pdf-proof: FAIL ")
		vi.Console(codeName(code))
		vi.Console("\n")
		vi.Exit(1)
	}
}
