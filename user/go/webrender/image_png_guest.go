//go:build virelai

package webrender

// The guest and host share the bounded in-tree decoder, not image/png.
func decodePNG(data []byte) (*Image, error) { return DecodePNG(data) }
