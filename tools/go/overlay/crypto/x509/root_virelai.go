//go:build virelai

package x509

// The guest ships no CA bundle and has no platform certificate store:
// every TLS verification runs against opts.Roots alone. Empty lists make
// SystemCertPool fall through to an empty pool — the same shape wasm
// takes (root_wasm.go). The matching dead systemVerify comes from
// root_unix.go, which apply.sh's vir_include selects for virelai; it is
// unreachable because verify.go only calls it on windows/darwin/ios.

// Possible certificate files; stop after finding one.
var certFiles = []string{}

// Possible directories with certificate files; all will be read.
var certDirectories = []string{}
