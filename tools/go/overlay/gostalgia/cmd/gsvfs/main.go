//go:build virelai

// gsvfs is the M95d (#2012) headless driver for the confined document
// store. Unlike gssmoke (which composes the stack over vfs.NewMem
// because the pin's host backend dies on Openat->ENOSYS), gsvfs runs the
// REAL boot path end to end: runtime.Boot -> InitRoot -> vfs.NewHost ->
// config.Load -> services -> session -> echo app, all riding the
// gsport/vfs host backend on /host. It then drives the fs service over
// the environment's own IPC — in-process Router.Dispatch with admin
// capabilities, the same call the boot self-test makes — so the fs/*
// routes, their RequireCap checks, and the VFS beneath them are the
// pinned code, unmodified.
//
// Modes (argv[1], default "save"):
//
//	save    — create a document, edit+save it, read it back hashed,
//	          persist a config value (gsport/vfs.Publish under
//	          config.saveLocked), exercise fs/mkdir + fs/remove, and
//	          probe every refusal class (dot-segment escape, the
//	          reserved "~" suffix, over-long and over-deep names).
//	drill   — write DRILL.TXT, then arm gvfs.PublishHook: at the
//	          "target-delete" stage (fsynced temp closed, live file
//	          gone, rename not yet run) it prints "drill-armed" and
//	          sleeps forever. The gate kills the process there — the
//	          worst crash point of the publish sequence.
//	recover — fresh boot over the same share; reading the drilled
//	          document completes the pending publish (gsvfs.RecoverHook
//	          marker) and returns the NEW bytes. Also proves the config
//	          value persisted across the kill boundary.
//
// Every marker line the go-gostalgia-files gate asserts is prefixed
// "gsvfs:". Refused probes print "gsvfs: refused <probe>" so the gate
// can count them by name.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"gostalgia/internal/ipc"
	"gostalgia/internal/runtime"
	"gostalgia/internal/security"

	gvfs "virelai/gsport/vfs"
)

const (
	defaultRoot = "/host/GSVFS"
	documents   = "/users/guest/documents"
	docPath     = documents + "/NOTE.TXT"
	drillPath   = documents + "/DRILL.TXT"
)

var (
	// The fixture files in go-gostalgia-files.spec pin these byte for
	// byte (share-equals); keep them pure ASCII so the pinned bytes are
	// what the gate's heredoc writes.
	docV1    = []byte("field journal\nfirst entry: the port holds.\n")
	docV2    = []byte("field journal\nfirst entry: the port holds.\nsecond entry: edited through fs/write, saved whole.\n")
	drillOld = []byte("drill old draft - recover must not resurrect me once the new publish commits.\n")
	drillNew = []byte("drill new draft - fsynced before the kill, published by the next reader.\n")
)

func marker(format string, args ...any) {
	fmt.Printf("gsvfs: "+format+"\n", args...)
}

func fatalf(format string, args ...any) {
	fmt.Printf("gsvfs: FAIL "+format+"\n", args...)
	os.Exit(70)
}

// client is the in-process IPC caller: Dispatch with the admin set, the
// capabilities a socket-authenticated client gets after the token
// handshake (server.go:143) — same routes, same RequireCap checks.
type client struct {
	rt  *runtime.Runtime
	ctx context.Context
	seq atomic.Int64
}

func (c *client) call(method string, params any, out any) error {
	pb, err := ipc.Encode(params)
	if err != nil {
		return err
	}
	resp := c.rt.Router.Dispatch(c.ctx, ipc.Request{
		ID:     c.seq.Add(1),
		Method: method,
		Params: pb,
	})
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("ipc: decode %s response: %w", method, err)
		}
	}
	return nil
}

// boot runs the pinned Boot path over the host backend.
func boot() (*runtime.Runtime, *client) {
	rt, err := runtime.Boot(context.Background(), runtime.Options{
		Root:      defaultRoot,
		LogOutput: os.Stdout,
	})
	if err != nil {
		fatalf("boot: %v", err)
	}
	c := &client{
		rt:  rt,
		ctx: ipc.WithCapabilities(context.Background(), security.AdminCapabilities()),
	}
	marker("ready endpoint=%s", rt.Endpoint())
	return rt, c
}

// shutdown asks for in-band shutdown through the real route, then waits
// for the runtime's own teardown to finish.
func shutdown(rt *runtime.Runtime, c *client, reason string) {
	_ = c.call("sys/shutdown", map[string]string{"reason": reason}, nil)
	rt.Wait()
}

func fsWrite(c *client, path string, data []byte) error {
	var out struct {
		Path    string `json:"path"`
		Written int    `json:"written"`
	}
	return c.call("fs/write",
		map[string]string{"path": path, "data_base64": base64.StdEncoding.EncodeToString(data)}, &out)
}

func fsRead(c *client, path string) ([]byte, error) {
	var out struct {
		Path string `json:"path"`
		Size int    `json:"size"`
		Data string `json:"data_base64"`
	}
	if err := c.call("fs/read", map[string]string{"path": path}, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Data)
}

func fsRemove(c *client, path string) error {
	return c.call("fs/remove", map[string]string{"path": path}, nil)
}

func fsMkdir(c *client, path string) error {
	return c.call("fs/mkdir", map[string]string{"path": path}, nil)
}

func fsList(c *client, path string, out any) error {
	return c.call("fs/list", map[string]string{"path": path}, out)
}

func main() {
	mode := "save"
	if len(os.Args) > 1 && os.Args[1] != "" {
		mode = os.Args[1]
	}
	switch mode {
	case "save":
		runSave()
	case "drill":
		runDrill()
	case "recover":
		runRecover()
	default:
		fatalf("unknown mode %q", mode)
	}
}

// runSave is the document lifecycle plus the refusal class evidence.
func runSave() {
	rt, c := boot()

	// Create, then edit+save the same document — both through fs/write,
	// both recoverable publishes on the share.
	if err := fsWrite(c, docPath, docV1); err != nil {
		fatalf("write doc: %v", err)
	}
	marker("wrote sha256=%x size=%d", sha256.Sum256(docV1), len(docV1))
	if err := fsWrite(c, docPath, docV2); err != nil {
		fatalf("edit doc: %v", err)
	}
	marker("edited sha256=%x", sha256.Sum256(docV2))
	back, err := fsRead(c, docPath)
	if err != nil {
		fatalf("read doc: %v", err)
	}
	if sha256.Sum256(back) != sha256.Sum256(docV2) {
		fatalf("readback mismatch: got %x want %x", sha256.Sum256(back), sha256.Sum256(docV2))
	}
	marker("readback sha256=%x", sha256.Sum256(back))

	// fs/remove + fs/mkdir through the service routes.
	if err := fsWrite(c, documents+"/TEMP.TXT", []byte("scratch\n")); err != nil {
		fatalf("write temp: %v", err)
	}
	if err := fsRemove(c, documents+"/TEMP.TXT"); err != nil {
		fatalf("remove temp: %v", err)
	}
	if err := fsMkdir(c, "/users/guest/scratch"); err != nil {
		fatalf("mkdir: %v", err)
	}
	marker("mkdir+remove ok")

	// Config persistence rides the same publish: Cfg.Set -> saveLocked
	// -> gvfs.Publish on config/system.json. Hash what landed.
	if err := rt.Cfg.Set("desktop.wallpaper", "dusk"); err != nil {
		fatalf("config set: %v", err)
	}
	cfgBytes, err := gvfs.ReadFile(defaultRoot + "/config/system.json")
	if err != nil {
		fatalf("config readback: %v", err)
	}
	marker("config sha256=%x size=%d", sha256.Sum256(cfgBytes), len(cfgBytes))

	// The refusal class — each is refused by name and must not touch the
	// share. The gate counts 'gsvfs: refused' and scans for strays.
	long := make([]byte, 0, 300)
	for len(long) < 260 {
		long = append(long, 'x')
	}
	probes := []struct {
		name string
		run  func() error
	}{
		{"escape-read", func() error {
			_, err := fsRead(c, "/users/../escape.txt")
			return err
		}},
		{"escape-write", func() error {
			return fsWrite(c, "/users/../../outside.txt", []byte("no\n"))
		}},
		{"tilde-suffix", func() error {
			return fsWrite(c, documents+"/DROP~", []byte("no\n"))
		}},
		{"overlong-name", func() error {
			return fsWrite(c, documents+"/"+string(long), []byte("no\n"))
		}},
		{"over-depth", func() error {
			return fsWrite(c, documents+"/a/b/c/d/deep.txt", []byte("no\n"))
		}},
	}
	for _, p := range probes {
		err := p.run()
		if err == nil {
			fatalf("%s was NOT refused", p.name)
		}
		marker("refused %s: %v", p.name, err)
	}

	shutdown(rt, c, "gsvfs-save")
	marker("done")
}

// runDrill arms the crash point: write the old draft clean, then start a
// publish of the new draft and freeze at "target-delete" — fsynced temp
// on the share, live file gone, rename never ran. The gate kills us here.
func runDrill() {
	// The runtime never shuts down: the process is killed mid-publish.
	_, c := boot()

	if err := fsWrite(c, drillPath, drillOld); err != nil {
		fatalf("drill old: %v", err)
	}
	marker("drill old sha256=%x", sha256.Sum256(drillOld))

	gvfs.PublishHook = func(stage string) {
		marker("staged %s", stage)
		if stage == "target-delete" {
			marker("drill-armed")
			// Sleep (not select{}): a parked timer keeps the deadlock
			// detector honest while the gate's kill lands.
			for {
				time.Sleep(time.Hour)
			}
		}
	}
	// Never returns: the process dies at the armed stage above.
	_ = fsWrite(c, drillPath, drillNew)
	fatalf("drill write returned — the kill never landed")
}

// runRecover reboots over the drilled share: the first read of DRILL.TXT
// completes the pending publish, and the bytes are the NEW draft.
func runRecover() {
	gvfs.RecoverHook = func(name string) {
		marker("recover publish %s", name)
	}
	rt, c := boot()

	back, err := fsRead(c, drillPath)
	if err != nil {
		fatalf("recover read: %v", err)
	}
	marker("recovered sha256=%x size=%d", sha256.Sum256(back), len(back))
	if sha256.Sum256(back) != sha256.Sum256(drillNew) {
		fatalf("recovered wrong bytes: got %x want %x", sha256.Sum256(back), sha256.Sum256(drillNew))
	}

	// The config value written before the kill must also be back.
	if got := rt.Cfg.String("desktop.wallpaper", ""); got != "dusk" {
		fatalf("config lost: wallpaper=%q", got)
	}
	marker("config persisted wallpaper=dusk")

	var out struct {
		Entries []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"is_dir"`
		} `json:"entries"`
	}
	if err := fsList(c, documents, &out); err != nil {
		fatalf("list: %v", err)
	}
	for _, e := range out.Entries {
		marker("dir-entry %s dir=%v", e.Name, e.IsDir)
	}

	shutdown(rt, c, "gsvfs-recover")
	marker("done")
}
