//go:build virelai

package main

func engine(a []byte) (int, int, uint8) {
	// Keep the identical fixture and capacities in the baseline; only PDF
	// and vector are omitted. The borrowed output occupies arena offset zero.
	if len(fixture) == 0 {
		return 0, 0, 1
	}
	p := words(a[:64*64*4])
	for i := range p {
		p[i] = 0xffffffff
	}
	return 64, 64, 0
}
