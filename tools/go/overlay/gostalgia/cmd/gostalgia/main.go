// Command gostalgia boots and serves the Gostalgia environment.
// pinned-sha256: 7f8d30b4a9c9a35cc2fef81d84fc6799819bbf163943d5609c61df71df98ec78
//
//	gostalgia shell [--root DIR] [--verbose] (default)
//	gostalgia boot [--root DIR] [--verbose]
//	gostalgia init [--root DIR]
//	gostalgia version
//
// M95c (#2011) Virelai variant: the dispatch and flag layout are the
// pin's; only the per-command bodies move. `shell` composes the runtime
// stack over the guest adapters and binds the Bubble Tea shell to a
// GOTABWM-hosted window tty (shell_virelai.go). `boot` and `init` route
// through runtime.Boot/InitRoot — os.OpenRoot and the *at calls the
// kernel refuses — so they decline by name until M95d's host-VFS card;
// a broken attempt would only surface the same ENOSYS less readably.
// The shutdown model is the gsport/sig seam, not signal.NotifyContext.
package main

import (
	"flag"
	"fmt"
	"os"

	"gostalgia/internal/runtime"
)

const usageText = `gostalgia — the Gostalgia environment runtime

Usage:
  gostalgia boot [--root DIR] [--verbose]   boot and serve the environment
  gostalgia shell [--root DIR] [--verbose] boot into the Charm terminal (default)
  gostalgia init [--root DIR]               create the environment root
  gostalgia version                         print the version
`

func main() {
	args := os.Args[1:]
	cmd := "shell"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	var err error
	switch cmd {
	case "boot":
		err = cmdBoot(args)
	case "shell":
		err = cmdShell(args)
	case "init":
		err = cmdInit(args)
	case "version", "--version", "-v":
		fmt.Println("gostalgia", runtime.Version)
	case "help", "--help", "-h":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "gostalgia: unknown command %q\n\n%s", cmd, usageText)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gostalgia:", err)
		os.Exit(1)
	}
}

func rootFlag(fs *flag.FlagSet) *string {
	return fs.String("root", "", "environment root directory (default $GOSTALGIA_ROOT or ~/.gostalgia)")
}

// cmdBoot declines by name: the headless server is the same runtime.Boot
// path the shell bypasses — InitRoot/vfs.NewHost reach os.OpenRoot and
// the *at calls the kernel refuses until M95d's host-VFS card.
func cmdBoot(args []string) error {
	fs := flag.NewFlagSet("boot", flag.ExitOnError)
	fs.String("root", "", "environment root directory")
	fs.Bool("verbose", false, "enable debug logging")
	fs.Parse(args)
	return fmt.Errorf("boot: the host-VFS runtime path is M95d's card — `gostalgia shell` composes the guest stack directly")
}

func cmdShell(args []string) error {
	fs := flag.NewFlagSet("shell", flag.ExitOnError)
	root := rootFlag(fs)
	verbose := fs.Bool("verbose", false, "enable debug logging to the runtime log")
	fs.Parse(args)
	return cmdShellVirelai(*root, *verbose)
}

// cmdInit declines by name for the same reason: runtime.InitRoot wraps
// os.OpenRoot/os.MkdirAll — no honest virelai answer until M95d.
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.String("root", "", "environment root directory")
	fs.Parse(args)
	return fmt.Errorf("init: environment-root creation is M95d's host-VFS card — the shell seeds its own root")
}
