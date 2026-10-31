//go:build !virelai

package testing

// Stock truncation bound for t.TempDir's name pattern — see
// tempdir_virelai.go for the platform that actually constrains it.
const tempDirNameMax = 64
