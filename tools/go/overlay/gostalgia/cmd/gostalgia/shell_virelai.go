//go:build virelai

// M95c (#2011): the `shell` command's guest composition. The pin's
// cmdShell delegates to runtime.Boot, whose InitRoot/vfs.NewHost ride
// os.OpenRoot and *at calls the kernel refuses — the host-VFS mapping is
// M95d's card. This file composes the same runtime stack the way the
// M95b gssmoke proves it: mem:// IPC, an in-memory environment VFS, the
// four services, one session, the echo app — and then binds the Bubble
// Tea v1 shell to a GOTABWM-hosted window tty through gsport/tty.
//
// Shutdown converges on ONE seam: `exit`/`shutdown` land on
// svcCtx.Shutdown, WIN_CLOSE and ^C/^D land on gsport/tty's sig
// Controller, and both feed the closed channel the shell model watches
// and the teardown below. Every marker the gate asserts is prefixed
// "gostalgia:" and written to the kernel console (fd 1) — never the
// tty, which the shell owns.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gostalgia/apps"
	"gostalgia/internal/app"
	"gostalgia/internal/config"
	"gostalgia/internal/events"
	"gostalgia/internal/experience/shell"
	"gostalgia/internal/ipc"
	"gostalgia/internal/process"
	"gostalgia/internal/runtime"
	"gostalgia/internal/security"
	"gostalgia/internal/service"
	"gostalgia/internal/services"
	"gostalgia/internal/session"
	"gostalgia/internal/vfs"
	"gostalgia/platform"

	"virelai/gsport/fsys"
	"virelai/gsport/tty"
)

const defaultShellRoot = "/host/GS"

func marker(format string, args ...any) {
	fmt.Printf("gostalgia: "+format+"\n", args...)
}

// loggedCaller mirrors every IPC call the shell makes onto the console —
// the serial half of the echo evidence (the grid half is the scanout).
type loggedCaller struct{ c shell.Caller }

func (l loggedCaller) Call(ctx context.Context, method string, in, out any) error {
	err := l.c.Call(ctx, method, in, out)
	if err != nil {
		marker("call %s err=%v", method, err)
	} else {
		marker("call %s ok", method)
	}
	return err
}

func cmdShellVirelai(root string, verbose bool) error {
	if root == "" {
		root = defaultShellRoot
	}

	// The backend: a GOTABWM-hosted window bound as the controlling
	// terminal (slot 23 open + slot 67 TtyWindow attach). The console
	// front-end was rejected — it opens no window, so nothing could
	// ever appear in the seat's window list (M95f's acceptance).
	input, output, err := tty.Open()
	if err != nil {
		return err
	}
	defer tty.Close()
	marker("tty attached window=%d", tty.WindowID())
	// Serial evidence of every delivered size: seeded cells first, then
	// one line per WIN_RESIZE the Program's event goroutine consumes.
	if c, r := tty.Size(); c > 0 {
		marker("size %dx%d", c, r)
	}
	tty.OnSize(func(c, r int) { marker("size %dx%d", c, r) })

	// The environment root is a host-share directory for runtime.json;
	// the environment VFS stays memory-backed (the host-VFS mount is
	// M95d's card).
	if err := fsys.Mkdir(root); err != nil {
		return fmt.Errorf("mkdir root: %w", err)
	}
	logLevel := slog.LevelWarn
	if verbose {
		logLevel = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	cfg, err := config.Load(filepath.Join(root, "config", "system.json"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	bus := events.NewBusWithLogger(log)
	router := ipc.NewRouter()
	env := vfs.New(vfs.NewMem())
	if err := env.Mount("/tmp", vfs.NewMem()); err != nil {
		return fmt.Errorf("mount /tmp: %w", err)
	}
	for _, dir := range []string{"/apps/manifests", "/users/guest", "/data"} {
		if err := env.MkdirAll(dir); err != nil {
			return fmt.Errorf("vfs seed %s: %w", dir, err)
		}
	}

	procs := process.NewManager(bus, log)
	sessions := session.NewManager(bus, log)
	registry := app.NewRegistry()
	if err := apps.Register(registry); err != nil {
		return fmt.Errorf("register apps: %w", err)
	}
	if err := apps.SeedManifests(env); err != nil {
		return fmt.Errorf("seed manifests: %w", err)
	}
	if _, err := registry.LoadManifests(env, "apps/manifests"); err != nil {
		return fmt.Errorf("load manifests: %w", err)
	}
	appMgr := app.NewManager(registry, procs, router, bus, log)

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return fmt.Errorf("token: %w", err)
	}
	token := hex.EncodeToString(tok)

	// The one shutdown seam: gsport/tty's Controller is where WIN_CLOSE
	// and ^C/^D land; svcCtx.Shutdown (sys/shutdown, `exit`, `shutdown`)
	// lands here first. Both close ctl.Done() — the channel shell.Run's
	// model watches — and both end in this teardown.
	ctl := tty.Shutdown()
	shutdownDone := make(chan struct{})
	var once sync.Once
	var sm *service.Manager
	shutdown := func(reason string) {
		once.Do(func() {
			defer close(shutdownDone)
			marker("shutdown reason=%s", reason)
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
		Version:  runtime.Version,
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
			return fmt.Errorf("register service: %w", err)
		}
	}
	if err := sm.StartAll(context.Background()); err != nil {
		return fmt.Errorf("start services: %w", err)
	}

	if _, err := sessions.Create(security.User{ID: "u-guest", Name: "guest"}); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	life, stop := ctl.NotifyContext(context.Background())
	defer stop()
	if _, err := appMgr.Launch(life, "com.gostalgia.echo"); err != nil {
		return fmt.Errorf("launch echo: %w", err)
	}
	marker("ready endpoint=%s root=%s", svcCtx.Endpoint, root)

	// The shell's client is in-process, so it can authenticate with the
	// very token the runtime minted above — no need to detour through
	// the runtime.json the IPC service published for external clients
	// (observed: the /host write flush is asynchronous, so reading it
	// back immediately is not reliable anyway).
	conn, err := platform.DialIPC(svcCtx.Endpoint)
	if err != nil {
		return fmt.Errorf("dial %s: %w", svcCtx.Endpoint, err)
	}
	client, err := ipc.NewClient(conn, token)
	if err != nil {
		return fmt.Errorf("client auth: %w", err)
	}
	defer client.Close()

	// The shell owns the tty pair now: input is the paced term.File that
	// forwards ^C/^D to the seam, output the bound /dev/tty the kernel
	// paints ANSI into. WithoutSignalHandler because the guest's
	// "signals" are the event queue the Program's resize goroutine
	// already owns — a second event poller would violate the ownership
	// contract, and SIGINT/SIGTERM never arrive anyway.
	//
	// The ctx handed to Run is deliberately NOT the seam's NotifyContext:
	// every quit path here is GRACEFUL — the model's watch returns
	// closedMsg on ctl.Done, ^C/^D arrive as key messages, `exit` and
	// WIN_CLOSE route through the same seam — and a context-done Program
	// reports ErrProgramKilled, which would turn every clean exit into
	// status 1. The pin's signal.NotifyContext has the same shape on
	// unix: ctx only kills when a real signal lands, which on virelai
	// never happens by construction. life (the ctl ctx) still paces the
	// echo app above — the one place a done ctx is the honest answer.
	runErr := shell.Run(context.Background(), loggedCaller{client}, ctl.Done(),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithoutSignalHandler())

	shutdown("shell exited")
	<-shutdownDone
	marker("done")
	return runErr
}
