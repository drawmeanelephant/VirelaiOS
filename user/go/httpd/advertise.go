// GOHTTPD.ELF --advertise NAME — M198b (#3361, index #3313): while the
// daemon serves on a real LAN attachment it announces the share by name
// over mDNS: "_http._tcp.local" PTR -> "NAME._http._tcp.local" SRV/TXT ->
// "NAME.local" A/AAAA, encoded by user/go/mdns and multicast to
// 224.0.0.251:5353 from the bound 5353 source port (RFC 6762 §5.1). The
// startup burst re-announces twice a second apart (§8.3's lost-packet
// cover), the steady state re-announces at TTL/2, and the clean-exit
// path sends the TTL=0 goodbye (§10.1).
//
// Honest bounds:
//
//   - Bridged-ness is not a kernel-visible flag — the guest cannot read
//     the host's attachment type. The observable bound is the VZ NAT
//     attachment's documented signature: it serves no DHCP and its
//     subnet is the observed 192.168.64.0/24 (docs/hardware-contract.md).
//     An own address inside that block — or no own address at all —
//     means no reachable LAN segment exists: "--advertise" refuses by
//     name ("httpd: advertise needs bridged") before the listener binds.
//     The --net capture attachment is NOT refused: it is a real wire
//     (the byte-exact capture is the class-B leg's evidence), and a
//     guest cannot tell a capture socket from a bridged NIC.
//   - The send rides the owned UDP seam (slot 84) through the M198b
//     kernel multicast branch: a 224.0.0.0/24 dst derives its group MAC
//     (no ARP) and leaves at IP TTL 255 — RFC 6762 §11's hard
//     requirement; Apple's responder discards anything less.
//   - AAAA carries the link-local address (fe80:: + the MAC-derived IID,
//     the same derivation kernel/src/ipv6.zig iid_from_mac applies): the
//     SLAAC global is kernel-internal with no EL0 read seam, so no
//     routable v6 is claimed where it cannot be observed. The record
//     shape — and the question it answers for a v6-capable browser — is
//     the card's.
package main

import (
	"virelai/mdns"
	"virelai/vi"
)

const (
	// advTTL is the record TTL (RFC 6762 §10's recommended 120 s for the
	// names-as-labels records); the shared advertiser re-announces at half it.
	advTTL = mdns.AdvTTL
)

// advSeam is the syscall surface the advertiser drives — injected so host
// tests run the whole announce/goodbye logic without the guest.
type advSeam struct {
	listen func(port uint16) int64                                    // vi.UDPListen
	send   func(dst [4]byte, dstPort, srcPort uint16, b []byte) int64 // vi.UDPSendFrom
	recv   func(port uint16, buf []byte) int64                        // vi.UDPRecvFrom
	close  func(port uint16) int64                                    // vi.UDPClose
	stats  func() (vi.NetStats, int64)                                // vi.NetStatsSnapshotRC
	sleep  func(ticks uint64)                                         // vi.Sleep
	log    func(line string)                                          // vi.ConsoleLine
}

func guestAdvSeam() advSeam {
	return advSeam{
		listen: vi.UDPListen,
		send:   vi.UDPSendFrom,
		recv:   vi.UDPRecvFrom,
		close:  vi.UDPClose,
		stats:  vi.NetStatsSnapshotRC,
		sleep:  vi.Sleep,
		log:    vi.ConsoleLine,
	}
}

// advertiser is one --advertise session: the name, the port the share is
// served on, the bound mDNS source port, and the record set as the wire
// speaks it.
type advertiser struct {
	name     string   // the instance label ("v198b-share")
	svcName  string   // NAME._http._tcp.local
	hostName string   // NAME.local — the SRV target / A+AAAA owner
	port     uint16   // the served TCP port (the SRV port)
	ip4      [4]byte  // the advertised A record — the guest's own IPv4
	ip6      [16]byte // the advertised AAAA record — the link-local
	seam     advSeam
	announce []byte // the encoded announce message (TTL = advTTL)
	goodbye  []byte // the same record set at TTL=0
	// M206b residual (#3814) / M214c (#3920): the shared mdns.Advertiser
	// owns the one :5353 bind, the burst, the bounded query responder and
	// the synchronized goodbye/close. HTTPD supplies only the service
	// description and its console markers.
	responder *mdns.Responder
	adv       *mdns.Advertiser
	started   bool
}

// validateAdvertiseName is the shared DNS-SD instance-label bound (kept as
// the HTTPD-local spelling its tests and call sites use).
func validateAdvertiseName(name string) bool { return mdns.ValidInstance(name) }

// onNatAttachment is the in-guest observable bound for "--advertise":
// the snapshot carries no own IPv4 at all, or the own address sits inside
// the VZ NAT attachment's observed 192.168.64.0/24 (a guest there has no
// LAN segment to announce on). A refused snapshot is no snapshot — a
// guest that cannot even read its own stats is not on a bridged boot
// either, so it refuses the same way.
func onNatAttachment(snap vi.NetStats, ok bool) bool {
	if !ok || snap.OwnIP == [4]byte{} {
		return true
	}
	return mdns.OnNatSubnet(snap.OwnIP)
}

// linkLocal6 derives the guest's link-local v6 the way the kernel does —
// fe80:: + the MAC's modified-EUI-64 IID (ipv6.iid_from_mac). The kernel
// forms it when the net device arms, so the address exists on every
// attachment this daemon serves on.
func linkLocal6(mac [6]byte) [16]byte { return mdns.LinkLocal6(mac) }

// newAdvertiser validates the name, snapshots the attachment check, and
// hands the service description to the shared mdns advertiser. A nil
// result is always preceded by its named console refusal — the caller exits.
func newAdvertiser(name string, port uint16, seam advSeam) *advertiser {
	if !validateAdvertiseName(name) {
		seam.log("httpd: advertise bad name")
		return nil
	}
	// #3444: --advertise declares `net` (this stats read plus the served
	// listener) and the `net-mdns` service right (the :5353 bind/sends) —
	// a refused grant is named, not folded into the bridged check.
	snap, statrc := seam.stats()
	if statrc == -vi.ErrEACCES {
		seam.log("httpd: advertise needs net")
		return nil
	}
	if onNatAttachment(snap, statrc == vi.NetStatsBytes) {
		seam.log("httpd: advertise needs bridged")
		return nil
	}
	a := &advertiser{
		name:     name,
		svcName:  name + "._http._tcp.local",
		hostName: name + ".local",
		port:     port,
		ip4:      snap.OwnIP,
		ip6:      linkLocal6(snap.MAC),
		seam:     seam,
	}
	adv := mdns.NewAdvertiser(mdns.Service{
		Type:     "_http._tcp.local",
		Instance: name,
		Host:     a.hostName,
		Port:     port,
		IPv4:     a.ip4,
		IPv6:     a.ip6,
		TTL:      advTTL,
		TXT:      []string{"txtvers=1", "path=/"},
	}, mdns.AdvSeam{
		Listen: seam.listen,
		Send:   seam.send,
		Recv:   seam.recv,
		Close:  seam.close,
		Sleep:  seam.sleep,
		Log:    func(line string) { seam.log("httpd: " + line) },
	})
	if adv == nil {
		return nil
	}
	a.adv = adv
	a.responder = adv.Responder()
	a.announce = adv.Announce()
	a.goodbye = adv.Goodbye()
	return a
}

// start binds the mDNS port through the shared advertiser, which starts
// the bounded query responder and the startup burst; the steady
// re-announce at TTL/2 runs on its own goroutine while serving.
func (a *advertiser) start() bool {
	if a.adv == nil || !a.adv.Start() {
		return false
	}
	a.started = true
	return true
}

// stop is the clean-exit half: the shared advertiser joins its burst and
// responder, sends the TTL=0 goodbye pair and releases the port.
func (a *advertiser) stop() {
	if !a.started || a.adv == nil {
		return
	}
	a.adv.Stop()
	a.started = false
}
