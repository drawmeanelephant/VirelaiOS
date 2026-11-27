package main

// Migration bindings: the FLINT CLI and FLINTD.ELF share one implementation
// in virelai/flintstore. These names keep the CLI's historical spellings
// (and its test corpus) on top of that single extracted core — they are
// aliases and one-line forwarders, not a second implementation.

import (
	"virelai/flintstore"
	"virelai/vi"
)

type (
	Span        = flintstore.Span
	StoredAtom  = flintstore.StoredAtom
	Op          = flintstore.Op
	ForkOrigin  = flintstore.ForkOrigin
	Stream      = flintstore.Stream
	Workspace   = flintstore.Workspace
	Change      = flintstore.Change
	StoreIndex  = flintstore.StoreIndex
	RefuseError = flintstore.RefuseError
	FSError     = flintstore.FSError
	DirEntry    = flintstore.DirEntry
	FS          = flintstore.FS
	UUIDGen     = flintstore.UUIDGen
	memFS       = flintstore.MemFS
)

// AtomKey uniquely identifies a declaration in a working tree by
// (file, kind, name).
type AtomKey struct {
	File string
	Kind string
	Name string
}

const (
	DefaultStoreRoot   = flintstore.DefaultStoreRoot
	MaxFileReadBytes   = flintstore.MaxFileReadBytes
	MaxAtoms           = flintstore.MaxAtoms
	MaxChanges         = flintstore.MaxChanges
	MaxLineBytes       = flintstore.MaxLineBytes
	MaxMessageBytes    = flintstore.MaxMessageBytes
	MaxStreamNameBytes = flintstore.MaxStreamNameBytes
	DefaultReadLimit   = flintstore.DefaultReadLimit
	MaxReadLimit       = flintstore.MaxReadLimit
)

// activeFS is the process-wide store backend (guestFS in the guest, MemFS in
// host tests).
var activeFS FS = flintstore.GuestFS{}

func refuse(code, detail string) error { return flintstore.Refuse(code, detail) }

func newMemFS() *memFS { return flintstore.NewMemFS() }

func cleanMemPath(p string) string { return flintstore.CleanPath(p) }

func canonShareDir(dir string) string { return flintstore.CanonShareDir(dir) }

func ensureDirAll(fs FS, path string) error { return flintstore.EnsureDirAll(fs, path) }

func ptrEqual(a, b *string) bool { return flintstore.PtrEqual(a, b) }

func copyStrPtr(p *string) *string { return flintstore.CopyStrPtr(p) }

func streamsDir(root string) string { return flintstore.StreamsDir(root) }

func streamRecordPath(root, id string) string { return flintstore.StreamRecordPath(root, id) }

func streamLogPath(root, id string) string { return flintstore.StreamLogPath(root, id) }

func workspacesDir(root string) string { return flintstore.WorkspacesDir(root) }

func workspaceDir(root, ws string) string { return flintstore.WorkspaceDir(root, ws) }

func workspaceConfigPath(root, ws string) string {
	return flintstore.WorkspaceConfigPath(root, ws)
}

func workspaceAtomsPath(root, ws string) string {
	return flintstore.WorkspaceAtomsPath(root, ws)
}

func workspaceStagePath(root, ws string) string {
	return flintstore.WorkspaceStagePath(root, ws)
}

func workspaceMergePath(root, ws string) string {
	return flintstore.WorkspaceMergePath(root, ws)
}

func ReadJSONLLines(data []byte) ([][]byte, error) { return flintstore.ReadJSONLLines(data) }

func LoadStoreIndex(fs FS, root string) (*StoreIndex, error) {
	return flintstore.LoadStoreIndex(fs, root)
}

func LoadWorkspaceAtoms(fs FS, root, ws string) ([]StoredAtom, error) {
	return flintstore.LoadWorkspaceAtoms(fs, root, ws)
}

func WriteWorkspaceAtoms(fs FS, root, ws string, atoms []StoredAtom) error {
	return flintstore.WriteWorkspaceAtoms(fs, root, ws, atoms)
}

func sortStoredAtoms(atoms []StoredAtom) { flintstore.SortStoredAtoms(atoms) }

func LoadWorkspaceStage(fs FS, root, ws string) ([]Op, error) {
	return flintstore.LoadWorkspaceStage(fs, root, ws)
}

func WriteWorkspaceStage(fs FS, root, ws string, ops []Op) error {
	return flintstore.WriteWorkspaceStage(fs, root, ws, ops)
}

func WriteJSONSafe(fs FS, path string, v any) error { return flintstore.WriteJSONSafe(fs, path, v) }

func opTypeCode(t string) uint8 { return flintstore.OpTypeCode(t) }

func sortOps(ops []Op) { flintstore.SortOps(ops) }

func ReadStreamChanges(fs FS, root, streamID string) ([]Change, error) {
	return flintstore.ReadStreamChanges(fs, root, streamID)
}

func WriteStreamRecord(fs FS, root string, st *Stream) error {
	return flintstore.WriteStreamRecord(fs, root, st)
}

func LoadStream(fs FS, root, streamID string) (*Stream, []Change, error) {
	return flintstore.LoadStream(fs, root, streamID)
}

func CheckStaleHead(fs FS, root, streamID string, expectedHead *string) (*Stream, error) {
	return flintstore.CheckStaleHead(fs, root, streamID, expectedHead)
}

func AppendStreamChange(fs FS, root, streamID string, expectedHead *string, c Change) (*Stream, error) {
	return flintstore.AppendStreamChange(fs, root, streamID, expectedHead, c)
}

func LoadAllStreamChanges(fs FS, root string, idx *StoreIndex) ([]Change, error) {
	return flintstore.LoadAllStreamChanges(fs, root, idx)
}

func ParseChange(line []byte) (Change, error) { return flintstore.ParseChange(line) }

func VerifyChange(c *Change) (bool, error) { return flintstore.VerifyChange(c) }

func MarshalChangeLine(c Change) ([]byte, error) { return flintstore.MarshalChangeLine(c) }

func sigMsg(c *Change) ([]byte, error) { return flintstore.SigMsg(c) }

func opBytes(op Op) ([]byte, error) { return flintstore.OpBytes(op) }

func opsHash(ops []Op) ([32]byte, error) { return flintstore.OpsHash(ops) }

func parseHex32Lower(s string) ([32]byte, bool) { return flintstore.ParseHex32Lower(s) }

func ParseUUID(s string) ([16]byte, bool) { return flintstore.ParseUUID(s) }

func FormatUUID(u [16]byte) string { return flintstore.FormatUUID(u) }

func IsUUIDv7(s string) bool { return flintstore.IsUUIDv7(s) }

func newUUIDGen(now func() int64, rand func([]byte) error) *UUIDGen {
	return flintstore.NewUUIDGen(now, rand)
}

func guestRandom(b []byte) error { return flintstore.GuestRandom(b) }

var defaultUUIDGen = flintstore.NewUUIDGen(vi.Now, guestRandom)
