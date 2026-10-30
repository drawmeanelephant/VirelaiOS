//go:build virelai

package testing

// The guest share's name grammar caps a path component at 31 bytes
// (kernel/src/file_table.zig's frozen Dirent row — a longer component is
// st_path_limit). t.TempDir derives its directory name from the test's
// full name plus os.MkdirTemp's random suffix (a uint32 — up to 10
// digits), so the pattern must stay under the cap or the follow-up
// os.Stat refuses ENAMETOOLONG and every TempDir user dies in
// testing.go's own frame (issue #3854).
const tempDirNameMax = 20
