// Package tty is the M95c (#2011) gsport adapter: it claims the calling
// process's controlling terminal for a GOTABWM-hosted window, binds it
// through sys_tty_attach's window front-end (ADR 0020 Amendment A), and
// hands Bubble Tea's Virelai overlays the two pieces of host state they
// cannot compute — "is this fd the bound /dev/tty" and "how many CELLS
// does the kernel grid hold now".
//
// Design (recorded on #2011 before implementation):
//
//   - Backend: the seat-bound window tty, not the kernel console. Every
//     shipped Charm app binds a window tty (charmhello, pulse); only a
//     hosted window can appear in the seat's window list, which M95f's
//     launcher acceptance requires. The console front-end opens no
//     window and cannot satisfy it.
//   - Event ownership: the Bubble Tea Program's listenForResize
//     goroutine owns the process's ADR 0009 queue (the v2 overlay's
//     contract, kept). Open drains nothing after hand-off; between bind
//     and Program start the kernel queue simply holds events.
//   - Resize: cells, never pixels — cols = clamp(w/cellW, 8, 80),
//     rows = (h-16)/cellH (tabapp.CellGrid's kernel-pinned formulas),
//     with the cell re-read per event because a font zoom moves the
//     rung (M80i). GetSize answers the last-known cells; there is no
//     TIOCGWINSZ to ask.
//   - Shutdown: WIN_CLOSE, ^C (0x03) and ^D (0x04) all land on the one
//     in-band seam — gsport/sig's Controller — never a signal.
//
// On the host every syscall returns -ENOSYS, so Open reports the refusal
// and the package stays plain unit-test surface.
package tty

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"virelai/gsport/abi"
	"virelai/gsport/sig"
	"virelai/tabapp"
)

// devTTY is the kernel's controlling-terminal path (file_table.is_tty_path).
const devTTY = "/dev/tty"

// state is process-scoped: one controlling terminal, one bound window.
var (
	mu     sync.Mutex
	tab    *tabapp.TabApp
	ttyFd  = -1
	in     *File
	out    *File
	ctl    = sig.New()
	cols   = 0
	rows   = 0
	onSize func(cols, rows int) // optional observer (gate markers)
)

// The kernel's hid_to_bytes encodes Return (usage 0x28) as LF, the
// console line editor's convention; a terminal front-end feeds Bubble
// Tea, whose key table names CR `enter` and LF `ctrl+j`. Read therefore
// maps 0x0a -> 0x0d — the one deliberate normalization — everywhere
// EXCEPT inside a DECSET-2004 bracketed paste, where a newline is
// pasted content, not a key (the same byte a real terminal delivers).
const (
	pasteBegin = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
	markerLen  = 6
)

// File is the term.File Bubble Tea v1 reads for input. The kernel's tty
// read is non-blocking (0 = empty queue; file_table read_handle's own
// comment), so Read paces itself — the tea read goroutine simply blocks
// here until the WM's key pump delivers bytes. Every returned buffer is
// scanned for ^C/^D (0x03/0x04): either byte requests shutdown on the
// seam, in addition to reaching the model as a key. out holds normalized
// bytes not yet handed to the caller; tail holds a trailing partial
// paste marker while its end is undecided.
type File struct {
	// f is the host-test handle only (os.Pipe). On virelai the File
	// carries the raw kernel fd and does I/O through abi slots: the
	// fork's os.File layer treats an O_RDWR handle as a positional file
	// (virPos/virCursor/virShadow bookkeeping) whose virPull drains the
	// tty input queue internally — observed eating every injected key —
	// and whose deferred write path would starve the window's output
	// ring. The kernel's tty read is a position-free queue drain, so
	// the raw syscall is the honest primitive.
	fd      int
	f       *os.File
	out     []byte
	tail    []byte
	inPaste bool
	scratch [512]byte
}

// readRaw is one device read: the queue drain on virelai, the pipe read
// on host.
func (f *File) readRaw(buf []byte) (int, error) {
	if f.f != nil {
		return f.f.Read(buf)
	}
	n, r := abi.FileRead(uint32(f.fd), buf)
	if r < 0 {
		return 0, abi.Check("tty_read", devTTY, r)
	}
	return n, nil
}

// writeRaw is one device write: the raw out-ring append on virelai, the
// pipe write on host.
func (f *File) writeRaw(p []byte) (int, error) {
	if f.f != nil {
		return f.f.Write(p)
	}
	n, r := abi.FileWriteAll(uint32(f.fd), p)
	if r < 0 {
		return n, abi.Check("tty_write", devTTY, r)
	}
	return n, nil
}

// inputSeen is the one-shot serial marker the gate reads as proof the
// kernel's key push reached the bound terminal (and this File drained
// it): 'tty: input n=<batch>' on the first non-empty read.
var inputSeen bool

func markFirstInput(n int) {
	if inputSeen {
		return
	}
	inputSeen = true
	fmt.Printf("tty: input n=%d\n", n)
}

// Read fills p from the terminal input queue, pacing empty polls.
func (f *File) Read(p []byte) (int, error) {
	for {
		if len(f.out) > 0 {
			n := copy(p, f.out)
			f.out = f.out[n:]
			return n, nil
		}
		n, err := f.readRaw(f.scratch[:])
		// The port's os.File.Read maps the kernel's "0 bytes this pass"
		// answer to io.EOF — on the level-triggered tty queue that is
		// "queue empty now", not end-of-file: /dev/tty has no EOF.
		// Propagating it ends Bubble Tea's readLoop on the first empty
		// poll, so the byte never has a reader. Keep polling instead.
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if n > 0 {
			markFirstInput(n)
		}
		src := make([]byte, 0, len(f.tail)+n)
		src = append(src, f.tail...)
		src = append(src, f.scratch[:n]...)
		out, tail, paste := translateEnter(src, f.inPaste)
		f.inPaste = paste
		f.tail = append(f.tail[:0], tail...)
		for _, b := range out {
			if b == 0x03 || b == 0x04 {
				ctl.RequestShutdown()
				break
			}
		}
		f.out = append(f.out, out...)
		if len(f.out) > 0 {
			continue
		}
		if err != nil {
			// Flush a truncated marker tail so every read byte reaches
			// the caller; the error surfaces on the next Read.
			f.out = append(f.out, f.tail...)
			f.tail = f.tail[:0]
			if len(f.out) > 0 {
				continue
			}
			return 0, err
		}
		if n == 0 {
			abi.Sleep(1)
		}
	}
}

// translateEnter applies the LF -> CR mapping over src; bytes inside a
// bracketed paste pass through verbatim. A trailing prefix of the
// expected marker (up to markerLen-1 bytes) is held back as tail for the
// next call, so a marker split across kernel reads is still recognized.
// pasteOut is the paste state after consuming src.
func translateEnter(src []byte, paste bool) (out, tail []byte, pasteOut bool) {
	for i := 0; i < len(src); {
		marker := []byte(pasteBegin)
		if paste {
			marker = []byte(pasteEnd)
		}
		rel := bytes.Index(src[i:], marker)
		end := len(src)
		if rel >= 0 {
			end = i + rel
		} else {
			for k := markerLen - 1; k > 0; k-- {
				if len(src)-i >= k && bytes.Equal(src[len(src)-k:], marker[:k]) {
					end = len(src) - k
					break
				}
			}
		}
		if paste {
			out = append(out, src[i:end]...)
		} else {
			for _, c := range src[i:end] {
				if c == '\n' {
					c = '\r'
				}
				out = append(out, c)
			}
		}
		if rel < 0 {
			return out, append(tail, src[end:]...), paste
		}
		// The marker itself passes through — the app parses the bracket.
		out = append(out, marker...)
		i = end + markerLen
		paste = !paste
	}
	return out, nil, paste
}

// Write appends to the terminal output ring through the raw slot —
// never os.File, whose fork shadow layer defers write-open handles.
func (f *File) Write(p []byte) (int, error) {
	return f.writeRaw(p)
}

// Close is a no-op: the terminal's lifecycle belongs to Close.
func (f *File) Close() error { return nil }

// Fd is the kernel file handle — the term.File contract.
func (f *File) Fd() uintptr {
	if f.f != nil {
		return f.f.Fd()
	}
	return uintptr(f.fd)
}

// Open hosts the app's window under the seat and binds the controlling
// terminal to it: win_open + declare_fullscreen (tabapp), open /dev/tty
// (slot 23 through abi.FileOpen — raw, so the fork records no positional
// state for the handle), then sys_tty_attach(TtyWindow, win) (slot 67).
// The kernel orders it the same way charmhello does: the attach fails
// EINVAL unless /dev/tty is already open. The last-known cell size seeds
// at the declared rect's grid; the seat's host-canvas grant arrives as
// the first WIN_RESIZE the Program's event goroutine consumes. Input and
// output share the one kernel handle — .tty reads drain the input ring,
// .tty writes append to the output ring — on separate File wrappers so
// the caller's io.Reader/io.Writer seams stay distinct. On the host the
// open fails inside tabapp.Init with ENOSYS.
func Open() (input *File, output *File, err error) {
	mu.Lock()
	defer mu.Unlock()
	if in != nil {
		return in, out, nil
	}
	ta := tabapp.Init(tabapp.Config{
		Name:  "GOSTALGIA.ELF",
		Title: "Gostalgia",
		X:     32,
		Y:     32,
		W:     640,
		H:     400,
	})
	if ta == nil {
		return nil, nil, fmt.Errorf("tty: win_open refused (no seat on this platform)")
	}
	h, r := abi.FileOpen(devTTY, abi.ModeRead|abi.ModeWrite)
	if r < 0 {
		ta.Close()
		return nil, nil, abi.Check("tty_open", devTTY, r)
	}
	if r := abi.TtyAttachWindow(ta.Win); r < 0 {
		abi.FileClose(uint32(h))
		ta.Close()
		return nil, nil, abi.Check("tty_attach", devTTY, r)
	}
	tab, ttyFd = ta, int(h)
	in = &File{fd: ttyFd}
	out = &File{fd: ttyFd}
	cw, ch := abi.TerminalCell()
	cols, rows = tabapp.CellGrid(ta.W, ta.H, cw, ch)
	return in, out, nil
}

// Shutdown is the process's in-band shutdown seam (gsport/sig). The cmd
// wiring hands it to shell.Run's closed channel and to the runtime's
// teardown; WIN_CLOSE and ^C/^D both land on it.
func Shutdown() *sig.Controller { return ctl }

// OnSize registers an observer for every size change (the resize leg's
// serial evidence). Called by NoteResize under the event goroutine.
func OnSize(fn func(cols, rows int)) {
	mu.Lock()
	onSize = fn
	mu.Unlock()
}

// Bound reports whether the controlling terminal is claimed (post-Open).
func Bound() bool {
	mu.Lock()
	defer mu.Unlock()
	return ttyFd >= 0
}

// IsBoundFd reports whether fd is the bound controlling terminal — the
// term.IsTerminal answer the Bubble Tea initInput path needs.
func IsBoundFd(fd uintptr) bool {
	mu.Lock()
	defer mu.Unlock()
	return ttyFd >= 0 && uintptr(ttyFd) == fd
}

// Size is the last-known terminal size in CELLS — term.GetSize's answer.
// Seeded at Open and refreshed by every WIN_RESIZE the Program's event
// goroutine consumes, so checkResize stays honest without an ioctl.
func Size() (w, h int) {
	mu.Lock()
	defer mu.Unlock()
	return cols, rows
}

// NoteResize applies a WIN_RESIZE pixel payload: re-reads the active
// cell rung (a font zoom moves it — M80i), recomputes the kernel's cell
// formulas, stores the answer for Size, notifies the observer, and
// returns the new cells for the WindowSizeMsg.
func NoteResize(wpx, hpx uint32) (int, int) {
	mu.Lock()
	defer mu.Unlock()
	cw, ch := abi.TerminalCell()
	cols, rows = tabapp.CellGrid(wpx, hpx, cw, ch)
	if onSize != nil {
		onSize(cols, rows)
	}
	return cols, rows
}

// NotifyClose is the WIN_CLOSE half of the event goroutine: the window's
// close gesture requests shutdown on the same seam ^C/^D land on. The
// Program quit itself is the caller's (it sends the QuitMsg).
func NotifyClose() { ctl.RequestShutdown() }

// WindowID is the hosted window's id (-1 before Open), for markers.
func WindowID() int {
	mu.Lock()
	defer mu.Unlock()
	if tab == nil {
		return -1
	}
	return tab.Win
}

// Close detaches the controlling terminal (slot 67 selector 0), closes
// the /dev/tty handle and the window. Idempotent; the cmd defers it.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if ttyFd >= 0 {
		_ = abi.TtyAttach(abi.TtyDetach)
		abi.FileClose(uint32(ttyFd))
		ttyFd = -1
		in, out = nil, nil
	}
	if tab != nil {
		tab.Close()
		tab = nil
	}
}
