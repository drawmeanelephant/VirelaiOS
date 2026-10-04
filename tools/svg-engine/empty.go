//go:build virelai && !svgfull

package main

func engine(a []byte, n int) (int, int, uint8) {
	// Baseline omits only engine imports/calls. The same arena and transport
	// remain retained, including the unused scene/workspace partitions.
	pix := words(a[sourceEnd:outputEnd])
	for i := 0; i < 64*64; i++ {
		pix[i] = 0
	}
	return 64, 64, 0
}
