// Command netstat is GONETSTAT.ELF, the M78a Go replacement for NETSTAT.BIN.
// It reads the frozen slot-62 snapshot and paints it into an ordinary guest
// window. The syscall and rendering paths are entirely guest-native: no host
// terminal, libc, or POSIX compatibility layer.
package main

import (
	"virelai/draw"
	"virelai/theme"
	"virelai/vi"
	"virelai/vsys"
	"virelai/widgets"
)

const (
	winX         = 40
	winY         = 40
	winW         = 512
	winH         = 384
	exitStatus   = 44
	refreshTicks = 4 // ~1 Hz at the Zig app's 250 ms scheduler quantum
)

// argvEnvpGuard keeps space for the kernel's fixed argv/envp block in the
// writable segment, as required by the GOOS=virelai loader/runtime contract.
var argvEnvpGuard [0x1000]byte

type canvas struct {
	draw.FillerCanvas
	filler vi.Filler
}

func ipString(ip [4]byte) string { return vi.FormatIPv4(ip) }

func macString(mac [6]byte) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 17)
	for i, v := range mac {
		if i != 0 {
			b[i*3-1] = ':'
		}
		b[i*3] = hex[v>>4]
		b[i*3+1] = hex[v&15]
	}
	return string(b)
}

func dhcpName(state byte) string {
	switch state {
	case 1:
		return "selecting"
	case 2:
		return "requesting"
	case 3:
		return "bound"
	case 4:
		return "renewing"
	case 5:
		return "rebinding"
	default:
		return "idle"
	}
}

func tcpName(state byte) string {
	switch state {
	case 1:
		return "SYN-SENT"
	case 2:
		return "ESTABLISHED"
	case 3:
		return "FIN-WAIT"
	case 4:
		return "CLOSED"
	default:
		return "IDLE"
	}
}

func drawLine(c *canvas, y int, label, value string, color uint32) {
	t := theme.Current
	left := widgets.Text{
		R:     widgets.Rect{X: 12, Y: y, W: 108, H: 14},
		Label: label,
		Fg:    t.Muted,
		Bg:    t.Bg,
	}
	left.Draw(c)
	right := widgets.Text{
		R:     widgets.Rect{X: 128, Y: y, W: 372, H: 14},
		Label: value,
		Fg:    color,
		Bg:    t.Bg,
	}
	right.Draw(c)
}

func drawSection(c *canvas, y int, name string) int {
	section := widgets.Text{
		R:     widgets.Rect{X: 12, Y: y, W: 488, H: 16},
		Label: name,
		Fg:    theme.Current.Accent,
		Bg:    theme.Current.Bg,
	}
	section.Draw(c)
	return y + 15
}

func drawSnapshot(c *canvas, s vi.NetStats) {
	t := theme.Current
	c.filler.Rect(c.WindowID, 0, 0, winW, winH, t.Bg)
	title := widgets.Text{
		R:     widgets.Rect{X: 12, Y: 8, W: 488, H: 20},
		Label: "GONETSTAT.ELF - network dashboard",
		Fg:    t.Accent,
		Bg:    t.Bg,
	}
	title.Draw(c)

	y := drawSection(c, 30, "Interface")
	drawLine(c, y, "MAC", macString(s.MAC), t.Text)
	y += 14
	drawLine(c, y, "IP", ipString(s.OwnIP), t.Text)
	y += 14
	drawLine(c, y, "GW", ipString(s.Gateway), t.Text)
	y += 17

	y = drawSection(c, y, "DHCP")
	drawLine(c, y, "State", dhcpName(s.DHCPState)+" ("+vi.Itoa64(int64(s.LeaseSecs))+" s lease)", t.Text)
	y += 14
	drawLine(c, y, "Lease IP", ipString(s.LeaseIP), t.Text)
	y += 17

	y = drawSection(c, y, "TCP")
	drawLine(c, y, "Peer", ipString(s.TCPPeerIP)+":"+vi.Itoa64(int64(s.TCPPeerPort))+" ("+tcpName(s.TCPState)+")", t.Text)
	y += 17

	y = drawSection(c, y, "UDP listeners")
	ports := "none"
	if s.UDPCount > 0 {
		ports = ""
		count := int(s.UDPCount)
		if count > len(s.UDPPorts) {
			count = len(s.UDPPorts)
		}
		for i := 0; i < count; i++ {
			if i > 0 {
				ports += " "
			}
			ports += vi.Itoa64(int64(s.UDPPorts[i]))
		}
	}
	drawLine(c, y, "Ports", ports, t.Text)
	y += 17

	y = drawSection(c, y, "ARP")
	if s.ARPCount == 0 {
		drawLine(c, y, "Table", "(empty)", t.Text)
		y += 14
	} else {
		count := int(s.ARPCount)
		if count > len(s.ARPIPs) {
			count = len(s.ARPIPs)
		}
		for i := 0; i < count && y < winH-32; i++ {
			drawLine(c, y, "ARP", ipString(s.ARPIPs[i])+" -> "+macString(s.ARPMACs[i]), t.Text)
			y += 14
		}
	}
	y += 3

	_ = drawSection(c, y, "Counters")
	y += 15
	drawLine(c, y, "TX", vi.Itoa64(int64(s.TXFrames))+" frames / "+vi.Itoa64(int64(s.TXBytes))+" B", t.Text)
	y += 14
	drawLine(c, y, "RX", vi.Itoa64(int64(s.RXFrames))+" frames / "+vi.Itoa64(int64(s.RXBytes))+" B ("+vi.Itoa64(int64(s.RXOverflow))+" dropped)", t.Text)
	c.filler.Flush()
}

func main() {
	argvEnvpGuard[0] = 1
	win, rc := vi.WinOpen(winX, winY, winW, winH)
	if rc < 0 {
		vi.ConsoleLine("netstat: failed to open window")
		vi.Exit(1)
	}
	vi.ConsoleLine("netstat: open id=" + vi.Itoa64(int64(win)))

	snap, ok := vi.NetStatsSnapshot()
	if !ok {
		snap = vi.NetStats{}
	}
	for _, section := range []string{"iface", "dhcp", "tcp", "udp", "arp", "counters"} {
		vi.ConsoleLine("netstat: section " + section)
	}
	c := &canvas{}
	c.Filler, c.WindowID = &c.filler, win
	drawSnapshot(c, snap)
	if vi.WinPresent(win) < 0 {
		vi.WinClose(win)
		vi.Exit(1)
	}
	vi.ConsoleLine("netstat: ready")
	if vsys.TimerSet(refreshTicks) < 0 {
		vi.WinClose(win)
		vi.Exit(1)
	}

	for {
		ev, result, ok := vi.PollEventRaw()
		if !ok {
			if result < 0 {
				vsys.TimerCancel()
				vi.WinClose(win)
				vi.Exit(1)
			}
			vi.Sleep(1)
			continue
		}
		if ev.Kind == vi.EvWinClose {
			break
		}
		if ev.Kind == vi.EvTimer {
			if next, valid := vi.NetStatsSnapshot(); valid {
				snap = next
			}
			drawSnapshot(c, snap)
			vi.WinPresent(win)
			if vsys.TimerSet(refreshTicks) < 0 {
				vsys.TimerCancel()
				vi.WinClose(win)
				vi.Exit(1)
			}
		}
	}
	vsys.TimerCancel()
	vi.WinClose(win)
	vi.Exit(exitStatus)
}
