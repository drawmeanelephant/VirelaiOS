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
	// names-as-labels records); the steady re-announce fires at half it.
	advTTL       = 120
	advBurstN    = 3 // startup announcements, ~1 s apart
	advBurstTick = 4 // ~1 s at the 250 ms scheduler quantum
	advHalfTicks = advTTL / 2 * 4
	// natSubnetByte2/3 pin the VZ NAT attachment's observed subnet —
	// 192.168.64.0/24, the one attachment with no reachable LAN segment.
	natSubnetByte0 = 192
	natSubnetByte1 = 168
	natSubnetByte2 = 64
)

// advSeam is the syscall surface the advertiser drives — injected so host
// tests run the whole announce/goodbye logic without the guest.
type advSeam struct {
	listen func(port uint16) int64                                    // vi.UDPListen
	send   func(dst [4]byte, dstPort, srcPort uint16, b []byte) int64 // vi.UDPSendFrom
	close  func(port uint16) int64                                    // vi.UDPClose
	stats  func() (vi.NetStats, bool)                                 // vi.NetStatsSnapshot
	sleep  func(ticks uint64)                                         // vi.Sleep
}

func guestAdvSeam() advSeam {
	return advSeam{
		listen: vi.UDPListen,
		send:   vi.UDPSendFrom,
		close:  vi.UDPClose,
		stats:  vi.NetStatsSnapshot,
		sleep:  vi.Sleep,
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
}

// validateAdvertiseName bounds the instance label to what the wire can
// carry and a browser can show: a non-empty DNS name, every label
// non-empty and ≤ 63 octets, total under the 255-octet name bound
// (mdns.appendName enforces the same bounds on encode — this check exists
// so a bad flag refuses by name before anything binds).
func validateAdvertiseName(name string) bool {
	if name == "" || len(name) > mdns.MaxNameWire {
		return false
	}
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i-start == 0 || i-start > 63 {
				return false
			}
			start = i + 1
		}
	}
	return true
}

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
	return snap.OwnIP[0] == natSubnetByte0 && snap.OwnIP[1] == natSubnetByte1 && snap.OwnIP[2] == natSubnetByte2
}

// linkLocal6 derives the guest's link-local v6 the way the kernel does —
// fe80:: + the MAC's modified-EUI-64 IID (ipv6.iid_from_mac). The kernel
// forms it when the net device arms, so the address exists on every
// attachment this daemon serves on.
func linkLocal6(mac [6]byte) [16]byte {
	var a [16]byte
	a[0] = 0xfe
	a[1] = 0x80
	a[8] = mac[0] ^ 0x02
	a[9] = mac[1]
	a[10] = mac[2]
	a[11] = 0xff
	a[12] = 0xfe
	a[13] = mac[3]
	a[14] = mac[4]
	a[15] = mac[5]
	return a
}

// advertiseRecords is the announce/goodbye record set at TTL ttl: the
// service PTR in the answer section with the instance's SRV + TXT, the
// host's A/AAAA as additionals — the RFC 6763 chain a browser resolves
// the instance through. cache-flush marks the instance/host records
// unique (they are — the fixed guest MAC makes the name ours alone).
func advertiseRecords(svcName, hostName string, port uint16, ip4 [4]byte, ip6 [16]byte, ttl uint32) (answers, additionals []mdns.Record) {
	answers = []mdns.Record{
		{Name: "_http._tcp.local", Type: mdns.TypePTR, TTL: ttl, PTR: svcName},
		{Name: svcName, Type: mdns.TypeSRV, TTL: ttl, Flush: true,
			SRV: mdns.SRV{Priority: 0, Weight: 0, Port: port, Target: hostName}},
		{Name: svcName, Type: mdns.TypeTXT, TTL: ttl, Flush: true,
			TXT: []string{"txtvers=1", "path=/"}},
	}
	additionals = []mdns.Record{
		{Name: hostName, Type: mdns.TypeA, TTL: ttl, Flush: true, A: ip4},
		{Name: hostName, Type: mdns.TypeAAAA, TTL: ttl, Flush: true, AAAA: ip6},
	}
	return answers, additionals
}

// newAdvertiser validates the name, snapshots the attachment check, and
// pre-encodes both messages. A nil result is always preceded by its named
// console refusal — the caller exits.
func newAdvertiser(name string, port uint16, seam advSeam) *advertiser {
	if !validateAdvertiseName(name) {
		vi.ConsoleLine("httpd: advertise bad name")
		return nil
	}
	snap, ok := seam.stats()
	if onNatAttachment(snap, ok) {
		vi.ConsoleLine("httpd: advertise needs bridged")
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
	ans, add := advertiseRecords(a.svcName, a.hostName, port, a.ip4, a.ip6, advTTL)
	msg, err := (&mdns.Message{Response: true, Authoritative: true, Answers: ans, Additionals: add}).Encode()
	if err != nil {
		vi.ConsoleLine("httpd: advertise encode failed")
		return nil
	}
	ans, add = advertiseRecords(a.svcName, a.hostName, port, a.ip4, a.ip6, 0)
	bye, err := (&mdns.Message{Response: true, Authoritative: true, Answers: ans, Additionals: add}).Encode()
	if err != nil {
		vi.ConsoleLine("httpd: advertise encode failed")
		return nil
	}
	a.announce = msg
	a.goodbye = bye
	return a
}

// start binds the mDNS port and fires the startup burst; the steady
// re-announce at TTL/2 runs on its own goroutine while serving. A bind
// refusal or a refused first send is named, never silent — the daemon
// exits rather than run unadvertised.
func (a *advertiser) start() bool {
	if rc := a.seam.listen(mdns.Port); rc < 0 {
		vi.ConsoleLine("httpd: advertise bind failed")
		return false
	}
	vi.ConsoleLine("httpd: advertising " + a.svcName + " port=" + vi.Itoa64(int64(a.port)))
	if !a.emit(a.announce) {
		return false
	}
	go func() {
		for i := 1; i < advBurstN; i++ {
			a.seam.sleep(advBurstTick)
			if !a.emit(a.announce) {
				return
			}
		}
		for {
			a.seam.sleep(advHalfTicks)
			if !a.emit(a.announce) {
				return
			}
		}
	}()
	return true
}

// emit sends one pre-encoded datagram to the mDNS group from the bound
// port; a refusal is logged with its errno, never dropped silently.
func (a *advertiser) emit(msg []byte) bool {
	rc := a.seam.send(mdns.Group4Bytes, mdns.Port, mdns.Port, msg)
	if rc < 0 {
		vi.ConsoleLine("httpd: advertise send rc=" + vi.Itoa64(rc))
		return false
	}
	vi.ConsoleLine("httpd: advertise sent")
	return true
}

// stop is the clean-exit half: the TTL=0 goodbye goes out twice
// back-to-back (lost-packet cover, the startup burst's reason) and the
// port releases.
func (a *advertiser) stop() {
	for i := 0; i < 2; i++ {
		if !a.emit(a.goodbye) {
			break
		}
	}
	vi.ConsoleLine("httpd: advertise goodbye sent")
	_ = a.seam.close(mdns.Port)
}
