//go:build !virelai

package webrender

// image/png is deliberately confined to the host test reference.
func decodePNG(data []byte) (*Image, error) { return DecodePNG(data) }
