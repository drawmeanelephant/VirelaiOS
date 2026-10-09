//go:build virelai

// gssmoke is the M95b headless smoke: it composes the Gostalgia runtime
// stack over an in-memory VFS — runtime.Boot's InitRoot/vfs.NewHost ride
// os.Root and *at calls the kernel refuses (the host-VFS adapter is
// M95d's card) — serves the environment's own IPC over the mem://
// transport, makes one authenticated client round trip to the echo app,
// exercises the gsport file/process/clock primitives against the /host
// share, then shuts down through the sys/shutdown route and exits 0.
//
// Every marker line the go-gostalgia gate asserts is prefixed "gssmoke:".
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gostalgia/apps"
	"gostalgia/internal/app"
	"gostalgia/internal/config"
	"gostalgia/internal/events"
	"gostalgia/internal/ipc"
	"gostalgia/internal/process"
	"gostalgia/internal/security"
	"gostalgia/internal/service"
	"gostalgia/internal/services"
	"gostalgia/internal/session"
	"gostalgia/internal/vfs"
	"gostalgia/platform"

	"virelai/gsport/fsys"
	"virelai/gsport/proc"
	"virelai/gsport/sig"
)

const defaultRoot = "/host/GSMOKE"

func marker(format string, args ...any) {
	fmt.Printf("gssmoke: "+format+"\n", args...)
}

func fatalf(format string, args ...any) {
	fmt.Printf("gssmoke: FAIL "+format+"\n", args...)
	os.Exit(70)
}

func main() {
	root := defaultRoot
	if len(os.Args) > 1 && os.Args[1] != "" {
		root = os.Args[1]
	}

	// The environment root is a host-share directory; create it through
	// the adapter, not os.MkdirAll — kernel path bounds are enforced
	// there, and a stale runtime.json from a dead run is not a live env.
	if err := fsys.Mkdir(root); err != nil {
		fatalf("mkdir root: %v", err)
	}
	if fsys.Exists(filepath.Join(root, "runtime.json")) {
		if _, err := fsys.ReadFile(filepath.Join(root, "runtime.json"), 1<<20); err == nil {
			// A prior run may have crashed before Stop removed the file:
			// it is stale evidence, not a peer — overwrite is fine.
			marker("note: stale runtime.json present")
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cfg, err := config.Load(filepath.Join(root, "config", "system.json"))
	if err != nil {
		fatalf("config: %v", err)
	}

	bus := events.NewBusWithLogger(log)
	router := ipc.NewRouter()

	// The whole environment filesystem is memory-backed for the smoke:
	// the /host share is exercised through gsport/fsys directly below.
	env := vfs.New(vfs.NewMem())
	if err := env.Mount("/tmp", vfs.NewMem()); err != nil {
		fatalf("mount /tmp: %v", err)
	}
	if err := env.MkdirAll("/apps/manifests"); err != nil {
		fatalf("vfs seed: %v", err)
	}

	procs := process.NewManager(bus, log)
	sessions := session.NewManager(bus, log)

	registry := app.NewRegistry()
	if err := apps.Register(registry); err != nil {
		fatalf("register apps: %v", err)
	}
	if err := apps.SeedManifests(env); err != nil {
		fatalf("seed manifests: %v", err)
	}
	if _, err := registry.LoadManifests(env, "apps/manifests"); err != nil {
		fatalf("load manifests: %v", err)
	}
	appMgr := app.NewManager(registry, procs, router, bus, log)

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		fatalf("token: %v", err)
	}
	token := hex.EncodeToString(tok)

	// The shutdown seam: gsport/sig's Controller is the in-band request
	// channel (virelai has no signals); sys/shutdown lands here.
	ctl := sig.New()
	shutdownDone := make(chan struct{})
	var once sync.Once
	var sm *service.Manager
	shutdown := func(reason string) {
		once.Do(func() {
			defer close(shutdownDone)
			// The request first lands on the sig seam — that is what
			// unblocks the echo app's Run via NotifyContext — then the
			// managers tear down in the pinned order.
			ctl.RequestShutdown()
			for id := range appMgr.Running() {
				_ = appMgr.Stop(id, 5*time.Second)
			}
			procs.StopAll(5 * time.Second)
			for _, sess := range sessions.Active() {
				_ = sessions.Close(sess.ID)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = sm.StopAll(ctx)
		})
	}

	svcCtx := &service.Context{
		Root:     root,
		Version:  "0.1.0-gssmoke",
		Config:   cfg,
		Events:   bus,
		Log:      log,
		Router:   router,
		VFS:      env,
		Procs:    procs,
		Apps:     appMgr,
		Sessions: sessions,
		Token:    token,
		BootedAt: time.Now(),
	}
	svcCtx.Shutdown = shutdown
	sm = service.NewManager(svcCtx, bus, log)
	svcCtx.Services = sm
	for _, s := range []service.Service{
		services.NewSys(),
		services.NewProc(),
		services.NewFS(),
		services.NewIPC(),
	} {
		if err := sm.Register(s); err != nil {
			fatalf("register service: %v", err)
		}
	}
	if err := sm.StartAll(context.Background()); err != nil {
		fatalf("start services: %v", err)
	}

	if _, err := sessions.Create(security.User{ID: "u-guest", Name: "guest"}); err != nil {
		fatalf("session: %v", err)
	}
	life, stop := ctl.NotifyContext(context.Background())
	defer stop()
	if _, err := appMgr.Launch(life, "com.gostalgia.echo"); err != nil {
		fatalf("launch echo: %v", err)
	}

	marker("ready endpoint=%s root=%s", svcCtx.Endpoint, root)

	// File primitives over the host share (slots 23/24/25/26/27/34/35/77).
	probe := filepath.Join(root, "probe.txt")
	f, err := fsys.Create(probe)
	if err != nil {
		fatalf("create: %v", err)
	}
	if _, err := f.Write([]byte("m95b smoke\n")); err != nil {
		fatalf("write: %v", err)
	}
	if err := f.Sync(); err != nil {
		fatalf("sync: %v", err)
	}
	if err := f.Close(); err != nil {
		fatalf("close: %v", err)
	}
	body, err := fsys.ReadFile(probe, 4096)
	if err != nil || string(body) != "m95b smoke\n" {
		fatalf("readback: %v body=%q", err, body)
	}
	entries, err := fsys.ReadDir(root)
	if err != nil {
		fatalf("readdir: %v", err)
	}
	if err := fsys.Rename(probe, filepath.Join(root, "probe2.txt")); err != nil {
		fatalf("rename: %v", err)
	}
	if err := fsys.Delete(filepath.Join(root, "probe2.txt")); err != nil {
		fatalf("delete: %v", err)
	}
	marker("file ok entries=%d", len(entries))

	// Process primitives over the share (slots 28 exec, 7 procs, 4 sleep).
	pid, err := proc.Spawn("HELLO.ELF")
	if err != nil {
		fatalf("spawn: %v", err)
	}
	status, err := proc.Wait(pid)
	if err != nil {
		fatalf("wait: %v", err)
	}
	marker("spawn pid=%d status=%d", pid, status)

	// Clock evidence: nanotime is CNTPCT_EL0, wall time slot 66 — both
	// already live in the fork's runtime, so no gsport/clock exists.
	marker("clock now=%d", time.Now().UnixNano())

	// One authenticated IPC round trip over mem:// through the real
	// server: DialIPC -> auth handshake -> app route.
	conn, err := platform.DialIPC(svcCtx.Endpoint)
	if err != nil {
		fatalf("dial %s: %v", svcCtx.Endpoint, err)
	}
	client, err := ipc.NewClient(conn, token)
	if err != nil {
		fatalf("auth: %v", err)
	}
	var echoOut struct {
		Msg    string `json:"msg"`
		Echoes int64  `json:"echoes"`
	}
	if err := client.Call(context.Background(), "app/com.gostalgia.echo/echo",
		map[string]string{"msg": "m95b smoke"}, &echoOut); err != nil {
		fatalf("echo call: %v", err)
	}
	marker("echo msg=%q echoes=%d", echoOut.Msg, echoOut.Echoes)

	// In-band shutdown request through the real route; the service stops
	// the listener mid-call, so the response is best-effort.
	marker("shutdown")
	_ = client.Call(context.Background(), "sys/shutdown",
		map[string]string{"reason": "gssmoke"}, &json.RawMessage{})
	select {
	case <-shutdownDone:
	case <-time.After(10 * time.Second):
		fatalf("shutdown never completed")
	}
	marker("done")
}
