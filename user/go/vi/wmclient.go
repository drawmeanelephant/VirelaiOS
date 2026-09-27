package vi

import (
	"sync"
	"sync/atomic"
)

// M56b (issue #1316): the tab-client WM_RPC wire + event dispatch. The frame
// is byte-identical to kernel/src/wnd_core.zig's `WmRpc` (mirrored in
// user/src/lib/ui/abi.zig): 38 bytes, little-endian, fitting the frozen
// 64-byte mailbox slot. A tab client never calls slot 65 (`wmctl`) — that
// seam is WM-only; an app asks the registered WM over the mailbox instead and
// polls its own inbox for the ack.
//
// The wire mirror is the integration drift guard: if it ever drifts from
// wnd_core, the WM's `wnd: mail` serve and the `wmrpc: *-ack` markers stop
// matching and the live gate fails. TestWmRpcWire pins it on the host.

const (
	// WmRpcTitleMax is the frame title width (wnd_core `wm_rpc_title_max`).
	WmRpcTitleMax = 24
	// WmRpcMax is the mailbox slot bound the frame must fit (wnd_core
	// `wm_rpc_max`).
	WmRpcMax = 64
	// WmRpcReplyFlag marks a WM ack frame (wnd_core `wm_rpc_reply_flag`).
	WmRpcReplyFlag uint8 = 0x80
)

// The frozen request kinds (wnd_core / abi.zig). Do not renumber.
const (
	WmRpcKindRaise             uint8 = 1
	WmRpcKindConfig            uint8 = 2
	WmRpcKindRegisterAction    uint8 = 3
	WmRpcKindInvokeAction      uint8 = 4
	WmRpcKindAttachTab         uint8 = 5
	WmRpcKindDetachTab         uint8 = 6
	WmRpcKindCycleTab          uint8 = 7
	WmRpcKindDeclareFullscreen uint8 = 8
	// Additive app-side nav kinds (user/src/lib/tabapp.zig): the WM routes
	// WM_RPC by mailbox without interpreting `kind`, so these need no
	// frozen-ABI change.
	WmRpcKindNavDeclare uint8 = 9
	WmRpcKindNavPoll    uint8 = 10
	// Additive client -> seat title update. The request title carries the
	// replacement; the ack keeps the same generic reply shape.
	WmRpcKindSetTitle uint8 = 11
	// M79k (#1720): the notify seam. The request title carries the message
	// text (24 NUL-padded bytes, the whole budget), and the request id is
	// the sender's own tab so the seat can click the toast back to it.
	WmRpcKindNotify uint8 = 12
	// M82b (#1769): key-only settings subscription and publish requests.
	// The seat sends kind 15 asynchronously to matching subscribers.
	WmRpcKindSettingsSubscribe uint8 = 13
	WmRpcKindSettingsPublish   uint8 = 14
	WmRpcKindSettingsChanged   uint8 = 15
)

// WmRpc is the 38-byte app-to-WM mailbox frame. Field order and widths are the
// kernel's: kind@0, id@1, seq@2, reply_to@3, applied@4, pad@5, x@6, y@8,
// w@10, h@12, title[24]@14.
//
// Frozen wire fields:
//   - kind: u8; bit 0x80 marks a reply, and additive kinds do not renumber
//   - id: u8 window id; the low byte only, so WmMailRequest refuses values > 0xff
//   - seq: u8 per-process monotonic request counter; zero is skipped
//   - reply_to: u8 requester pid; the low byte only, so values > 0xff are refused
//   - applied: u8, reply frames only
//   - x, y, w, h: u16 little-endian
//   - title: 24 NUL-padded bytes; set-title requests carry the tab title and
//     nav-poll replies carry their path
//
// The id and reply_to bounds are part of the client contract, not an
// invitation to truncate a wider value silently.
type WmRpc struct {
	Kind    uint8
	ID      uint8
	Seq     uint8
	ReplyTo uint8
	Applied uint8
	Pad     uint8
	X       uint16
	Y       uint16
	W       uint16
	H       uint16
	Title   [WmRpcTitleMax]byte
}

// Encode marshals the frame little-endian into a fresh 38-byte slice. Hand
// encoded (not an unsafe cast) so the byte layout is explicit and does not
// depend on the compiler's struct padding.
func (m WmRpc) Encode() []byte {
	b := make([]byte, 38)
	b[0] = m.Kind
	b[1] = m.ID
	b[2] = m.Seq
	b[3] = m.ReplyTo
	b[4] = m.Applied
	b[5] = m.Pad
	putU16(b[6:], m.X)
	putU16(b[8:], m.Y)
	putU16(b[10:], m.W)
	putU16(b[12:], m.H)
	copy(b[14:38], m.Title[:])
	return b
}

// DecodeWmRpc parses a frame from b, which must be at least 38 bytes.
func DecodeWmRpc(b []byte) (WmRpc, bool) {
	if len(b) < 38 {
		return WmRpc{}, false
	}
	var m WmRpc
	m.Kind = b[0]
	m.ID = b[1]
	m.Seq = b[2]
	m.ReplyTo = b[3]
	m.Applied = b[4]
	m.Pad = b[5]
	m.X = getU16(b[6:])
	m.Y = getU16(b[8:])
	m.W = getU16(b[10:])
	m.H = getU16(b[12:])
	copy(m.Title[:], b[14:38])
	return m, true
}

// SetTitle copies up to WmRpcTitleMax bytes of s into the frame's title,
// leaving the remainder NUL-padded.
func (m *WmRpc) SetTitle(s string) {
	for i := range m.Title {
		m.Title[i] = 0
	}
	n := len(s)
	if n > WmRpcTitleMax {
		n = WmRpcTitleMax
	}
	copy(m.Title[:n], s[:n])
}

// TitleString returns the frame's title with the NUL padding trimmed.
func (m WmRpc) TitleString() string {
	n := 0
	for n < len(m.Title) && m.Title[n] != 0 {
		n++
	}
	return string(m.Title[:n])
}

// wmMailWaitTicks bounds one WM_RPC ack wait. Each miss parks one scheduler
// tick (slot 4 sys_sleep; 1 s on VZ) so a silent seat cannot burn millions
// of yield-spins inside tabapp.Init (#1489). The first probe is immediate —
// a WM that has already replied never sleeps. Expiry is false (honest
// refusal; DeclareFullscreen callers already handle it).
const wmMailWaitTicks = 8

// wmSeq is process-local because the package-level helpers are the client
// surface: there is no Client object to own the counter. Atomic allocation
// keeps two goroutines making requests from receiving the same sequence.
var wmSeq atomic.Uint32

// A blocking WM_RPC request can receive another request's ack or a
// settings-change frame while it waits. Preserve both for the rightful
// consumer; the shared mailbox remains safe when an app has concurrent RPC
// callers and an appkit loop polling asynchronous settings.
var (
	wmInboxMu      sync.Mutex
	pendingWmInbox [MailboxMaxMessages]WmRpc
	pendingWmCount int
)

func enqueueWmMessage(m WmRpc) {
	wmInboxMu.Lock()
	defer wmInboxMu.Unlock()
	if m.Kind == WmRpcKindSettingsChanged {
		for i := 0; i < pendingWmCount; i++ {
			old := pendingWmInbox[i]
			if old.Kind == m.Kind && old.ID == m.ID && old.TitleString() == m.TitleString() {
				pendingWmInbox[i] = m
				return
			}
		}
	}
	if pendingWmCount == len(pendingWmInbox) {
		copy(pendingWmInbox[:], pendingWmInbox[1:])
		pendingWmCount--
	}
	pendingWmInbox[pendingWmCount] = m
	pendingWmCount++
}

func takePendingWmMessage(matches func(WmRpc) bool) (WmRpc, bool) {
	wmInboxMu.Lock()
	defer wmInboxMu.Unlock()
	for i := 0; i < pendingWmCount; i++ {
		m := pendingWmInbox[i]
		if !matches(m) {
			continue
		}
		copy(pendingWmInbox[i:], pendingWmInbox[i+1:pendingWmCount])
		pendingWmCount--
		pendingWmInbox[pendingWmCount] = WmRpc{}
		return m, true
	}
	return WmRpc{}, false
}

func receiveWmMessage() (WmRpc, bool) {
	var raw [WmRpcMax]byte
	wmInboxMu.Lock()
	n, r := IpcRecv(raw[:])
	wmInboxMu.Unlock()
	if r < 0 || n < 38 {
		return WmRpc{}, false
	}
	return DecodeWmRpc(raw[:n])
}

func nextWmSeq() uint8 {
	for {
		seq := wmSeq.Add(1)
		if seq&0xff != 0 {
			return uint8(seq)
		}
	}
}

func fitsWire8(v uint32) bool {
	return v <= 0xff
}

// waitWmRpcAck polls the caller's inbox for a matching WM_RPC ack. A silent
// mailbox parks between probes and returns (_, false) when the tick budget
// runs out; it never yield-spins. Replies with another sequence or requester
// are foreign and are discarded rather than accepted as this request's ack.
func waitWmRpcAck(seq, replyTo uint8) (WmRpc, bool) {
	for tick := uint64(0); tick < wmMailWaitTicks; tick++ {
		if rep, ok := takePendingWmMessage(func(m WmRpc) bool {
			return m.Kind&WmRpcReplyFlag != 0 && m.Seq == seq && m.ReplyTo == replyTo
		}); ok {
			return rep, true
		}
		if m, ok := receiveWmMessage(); ok {
			if m.Kind&WmRpcReplyFlag != 0 && m.Seq == seq && m.ReplyTo == replyTo {
				return m, true
			}
			enqueueWmMessage(m)
		}
		if tick+1 >= wmMailWaitTicks {
			return WmRpc{}, false
		}
		_ = svc1(SlotSleep, 1)
	}
	return WmRpc{}, false
}

// WmMailRequest sends one WM_RPC request to the registered WM and polls the
// CALLER's own inbox for the matching ack. It returns whether the WM applied
// it. A missing WM seat, an id or requester pid that does not fit the frozen
// 8-bit wire fields, or a bounded-poll timeout returns false (honest — the
// caller then falls back to the frozen syscall); recv reads the caller's own
// ring, while the seat uses reply_to to route the ack.
func WmMailRequest(kind uint8, id uint32, x, y, w, h uint16, title, selfName string) bool {
	if !fitsWire8(id) {
		return false
	}
	peers := WmPeers(selfName)
	if peers.WM == 0 || peers.Self == 0 || !fitsWire8(peers.Self) {
		return false
	}
	req := WmRpc{
		Kind:    kind,
		ID:      uint8(id),
		Seq:     nextWmSeq(),
		ReplyTo: uint8(peers.Self),
		X:       x,
		Y:       y,
		W:       w,
		H:       h,
	}
	req.SetTitle(title)
	frame := req.Encode()
	if IpcSend(peers.WM, frame) < 0 {
		return false
	}
	rep, ok := waitWmRpcAck(req.Seq, req.ReplyTo)
	if !ok {
		return false
	}
	return rep.Applied != 0
}

// DeclareFullscreen asks the WM to make this tab full-viewport eligible (kind
// 8). TABWM accepts; WND.BIN and the shim refuse and the app keeps its legacy
// size — the zero-regression path.
func DeclareFullscreen(winID uint32, title, selfName string) bool {
	return WmMailRequest(WmRpcKindDeclareFullscreen, winID, 0, 0, 0, 0, title, selfName)
}

// DeclareNav tells the WM this tab navigated to path (kind 9) — the
// browser-history analogue for FILE/EDIT tabs.
func DeclareNav(winID uint32, path, selfName string) bool {
	if path == "" {
		return false
	}
	return WmMailRequest(WmRpcKindNavDeclare, winID, 0, 0, 0, 0, path, selfName)
}

// PollNav asks the WM for a back/forward target the user picked on the rail
// (kind 10), returning the path when one is queued. Best-effort: a missing WM
// seat or no pending target returns ("", false).
func PollNav(winID uint32, selfName string) (string, bool) {
	if !fitsWire8(winID) {
		return "", false
	}
	peers := WmPeers(selfName)
	if peers.WM == 0 || peers.Self == 0 || !fitsWire8(peers.Self) {
		return "", false
	}
	req := WmRpc{
		Kind:    WmRpcKindNavPoll,
		ID:      uint8(winID),
		Seq:     nextWmSeq(),
		ReplyTo: uint8(peers.Self),
	}
	frame := req.Encode()
	if IpcSend(peers.WM, frame) < 0 {
		return "", false
	}
	rep, ok := waitWmRpcAck(req.Seq, req.ReplyTo)
	if !ok || rep.Applied == 0 {
		return "", false
	}
	return rep.TitleString(), true
}

// SetTabTitle asks the seat to replace the title on winID (kind 11). An empty
// title is refused before the mailbox: every open tab should remain named.
func SetTabTitle(winID uint32, title, selfName string) bool {
	if title == "" {
		return false
	}
	return WmMailRequest(WmRpcKindSetTitle, winID, 0, 0, 0, 0, title, selfName)
}

// Notify tells the seat to show a toast for winID (kind 12). The whole
// message budget is the frame's 24-byte title, so text is a short human
// string ("copied notes.txt"), not a path or a payload -- the same bound
// SetTabTitle and DeclareNav already live inside. Best-effort: a missing
// seat, an id that does not fit the 8-bit wire, or a refused/timeout ack
// returns false, and the caller should say so rather than assume the user
// was told.
func Notify(winID uint32, text, selfName string) bool {
	if text == "" {
		return false
	}
	return WmMailRequest(WmRpcKindNotify, winID, 0, 0, 0, 0, text, selfName)
}

// SubscribeSetting asks the seat to send kind-15 notices when key changes.
// Keys longer than the frame title are refused rather than subscribed under a
// truncated name.
func SubscribeSetting(winID uint32, key, selfName string) bool {
	if key == "" || len(key) > WmRpcTitleMax {
		return false
	}
	return WmMailRequest(WmRpcKindSettingsSubscribe, winID, 0, 0, 0, 0, key, selfName)
}

// PublishSettingChange tells the seat a persisted key changed. The seat
// validates the key against SETTINGS.TXT, then fans out kind-15 notices.
func PublishSettingChange(winID uint32, key, selfName string) bool {
	if key == "" || len(key) > WmRpcTitleMax {
		return false
	}
	return WmMailRequest(WmRpcKindSettingsPublish, winID, 0, 0, 0, 0, key, selfName)
}

// PollSettingChanged returns the next asynchronous key notice for winID.
// It never waits; ordinary appkit polling remains paced by the event loop.
func PollSettingChanged(winID uint32) (string, bool) {
	if !fitsWire8(winID) {
		return "", false
	}
	if m, ok := takePendingWmMessage(func(m WmRpc) bool {
		return m.Kind == WmRpcKindSettingsChanged && m.ReplyTo == 0 &&
			m.ID == uint8(winID) && m.TitleString() != ""
	}); ok {
		return m.TitleString(), true
	}
	for i := 0; i < MailboxMaxMessages; i++ {
		m, ok := receiveWmMessage()
		if !ok {
			return "", false
		}
		if m.Kind == WmRpcKindSettingsChanged && m.ReplyTo == 0 && m.TitleString() != "" && m.ID == uint8(winID) {
			return m.TitleString(), true
		}
		enqueueWmMessage(m)
	}
	return "", false
}

func resetSettingChangeQueue() {
	wmInboxMu.Lock()
	pendingWmInbox = [MailboxMaxMessages]WmRpc{}
	pendingWmCount = 0
	wmInboxMu.Unlock()
}

// WmAction is what a tab client's event dispatch decided to do.
type WmAction int

const (
	// WmActionNone: not a WM-lifecycle event (mouse/key/timer...).
	WmActionNone WmAction = iota
	// WmActionResized: the WM resized the canvas; relayout at (W, H).
	WmActionResized
	// WmActionClosed: the window closed; clean up and exit.
	WmActionClosed
)

// TabClient is the state the dispatch loop tracks: the window id and the
// CURRENT canvas size, which follows every WIN_RESIZE (the WM's SET_WINDOW
// seam). Apps lay out against these, never against compile-time constants,
// once tab-aware.
type TabClient struct {
	Win     uint32
	W       uint32
	H       uint32
	Focused bool
}

// NewTabClient returns a client for winID with the given initial canvas size.
func NewTabClient(winID, w, h uint32) *TabClient {
	return &TabClient{Win: winID, W: w, H: h}
}

// Dispatch classifies one WM-lifecycle event and tracks the canvas.
// WIN_RESIZE updates the tracked size and returns WmActionResized; WIN_CLOSE
// returns WmActionClosed; WIN_FOCUS/WIN_BLUR update the focus flag. Everything
// else is WmActionNone and stays the app's own business.
func (t *TabClient) Dispatch(ev Event) WmAction {
	switch ev.Kind {
	case EvWinClose:
		return WmActionClosed
	case EvWinResize:
		t.W = ev.Arg0
		t.H = ev.Arg1
		return WmActionResized
	case EvWinFocus:
		t.Focused = true
	case EvWinBlur:
		t.Focused = false
	}
	return WmActionNone
}
