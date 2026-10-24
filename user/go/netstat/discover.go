// GONETSTAT.ELF `discover [service-type]` — M198c (#3383, index #3313):
// the guest browses the LAN. One bounded mDNS browse over the owned UDP
// seam: a QU PTR query for the service type — or, with no argument, the
// DNS-SD meta-name, whose answers enumerate the LAN's advertised types
// and are then browsed in turn (bounded) — sent to 224.0.0.251:5353
// from the bound 5353 source port. Replies collected inside the window
// are deduped into the cache and the PTR -> SRV/TXT (-> A) chain is
// printed per service.
//
// Honest bounds:
//
//   - QU=1 is not an optimization; it is the only reply shape this stack
//     delivers. The IPv4 dst gate (kernel/src/ipv4.zig) admits own-IP
//     and 255.255.255.255 only — a multicast-addressed reply is counted
//     at the MAC layer (M198a's rx_mdns) and dropped before EL0. RFC
//     6762 §5.4/§8.1 license the one-shot unicast-response query; that
//     is what this verb sends.
//   - The source port is the bound 5353 — the canonical mDNS shape and
//     the refusal the verb names when a running --advertise already owns
//     it (a duplicate bind is EINVAL).
//   - The window is hard-bounded: two sends per query name, a fixed
//     tick count, a per-tick drain cap, the slot-85 record bound — a
//     reply whose payload exceeds 512 B arrives truncated, fails
//     decode, and is counted, never trusted.
//   - TTL=0 goodbyes withdraw what they name, in wire order: a service
//     announced then withdrawn inside the window does not print.
//   - "offline" (no snapshot / no own IP) and "needs bridged" (the VZ
//     NAT signature 192.168.64.0/24 — the one observed attachment with
//     no LAN segment for multicast to reach, the same observable bound
//     --advertise names) refuse before the bind.
package main

import (
	"sort"
	"strings"

	"virelai/mdns"
	"virelai/vi"
)

const (
	// discMetaName is the DNS-SD service-type enumeration name (RFC
	// 6763 §9): its PTR answers are the types the LAN advertises.
	discMetaName = "_services._dns-sd._udp.local"
	// discWindowTicks is the per-phase listen window (~3 s at the 250 ms
	// scheduler quantum); each name is sent at t=0 and again at
	// discResendTick — the lost-packet cover a one-shot query is allowed.
	discWindowTicks = 12
	discResendTick  = 6
	// discDrainMax bounds the datagrams one poll tick consumes; the
	// rest wait in the kernel ring for the next tick.
	discDrainMax = 8
	// discMaxTypes bounds the bare verb's second phase: the LAN may
	// advertise a dozen types; the browse asks for four of them.
	discMaxTypes = 4
	// discRecvBuf is the slot-85 record bound: 4-byte source prefix,
	// 8-byte UDP header, up to 512 payload bytes.
	discRecvBuf = 4 + 8 + 512
	// natSubnetByte* pin the VZ NAT attachment's observed subnet —
	// 192.168.64.0/24, the one attachment with no reachable LAN segment
	// (the same signature --advertise refuses on).
	natSubnetByte0 = 192
	natSubnetByte1 = 168
	natSubnetByte2 = 64

	discKindType    = iota + 1 // a PTR answer to the meta query — a service TYPE
	discKindService            // a PTR answer to a type query — an instance
)

// discSeam is the syscall surface the browser drives — injected so host
// tests run the whole query/drain/fold logic without the guest (the
// advertiser's advSeam shape).
type discSeam struct {
	listen func(port uint16) int64                                    // vi.UDPListen
	send   func(dst [4]byte, dstPort, srcPort uint16, b []byte) int64 // vi.UDPSendFrom
	recv   func(port uint16, buf []byte) int64                        // vi.UDPRecvFrom
	close  func(port uint16) int64                                    // vi.UDPClose
	stats  func() (vi.NetStats, bool)                                 // vi.NetStatsSnapshot
	sleep  func(ticks uint64)                                         // vi.Sleep
	log    func(line string)                                          // vi.ConsoleLine
}

func guestDiscSeam() discSeam {
	return discSeam{
		listen: vi.UDPListen,
		send:   vi.UDPSendFrom,
		recv:   vi.UDPRecvFrom,
		close:  vi.UDPClose,
		stats:  vi.NetStatsSnapshot,
		sleep:  vi.Sleep,
		log:    vi.ConsoleLine,
	}
}

// foldName is the cache key form — EqualName's ASCII case-fold applied
// so "Gate-WS" and "gate-ws" dedupe to one service.
func foldName(name string) string {
	return strings.ToLower(name)
}

// discSvc is one deduped cache entry: the instance (or, for the meta
// phase, the service type) a PTR pointed at, plus the SRV/TXT records
// that resolved it. The kind records WHICH name's PTR created it.
type discSvc struct {
	name    string
	kind    int
	target  string
	port    uint16
	haveSRV bool
	txt     []string
	haveTXT bool
}

// discHost is the address half of the cache: A/AAAA records fold by the
// SRV target's name, not by the service that pointed at it.
type discHost struct {
	a    map[[4]byte]bool
	aaaa map[[16]byte]bool
}

// discCache is the deduped browse state: every accepted message folds
// into it, so a re-announce updates in place and a goodbye withdraws.
// replies/ignored are the observability counters the done-line prints.
type discCache struct {
	svcs    map[string]*discSvc
	hosts   map[string]*discHost
	replies int
	ignored int
}

func newDiscCache() *discCache {
	return &discCache{svcs: map[string]*discSvc{}, hosts: map[string]*discHost{}}
}

func (c *discCache) host(name string) *discHost {
	k := foldName(name)
	h := c.hosts[k]
	if h == nil {
		h = &discHost{a: map[[4]byte]bool{}, aaaa: map[[16]byte]bool{}}
		c.hosts[k] = h
	}
	return h
}

// foldRecord applies one wire record to the cache in wire order. TTL=0
// is the goodbye (RFC 6762 §10.1): the named entry's corresponding fact
// is withdrawn, not printed.
func (c *discCache) foldRecord(r mdns.Record) {
	switch r.Type {
	case mdns.TypePTR:
		k := foldName(r.PTR)
		if r.TTL == 0 {
			delete(c.svcs, k)
			return
		}
		s := c.svcs[k]
		if s == nil {
			s = &discSvc{name: r.PTR, kind: discKindService}
			if mdns.EqualName(r.Name, discMetaName) {
				s.kind = discKindType
			}
			c.svcs[k] = s
		} else if s.kind == discKindType && !mdns.EqualName(r.Name, discMetaName) {
			s.kind = discKindService // an instance answer upgrades a type answer
		}
	case mdns.TypeSRV:
		if s := c.svcs[foldName(r.Name)]; s != nil {
			if r.TTL == 0 {
				s.haveSRV = false
				return
			}
			s.target, s.port, s.haveSRV = r.SRV.Target, r.SRV.Port, true
		}
	case mdns.TypeTXT:
		if s := c.svcs[foldName(r.Name)]; s != nil {
			if r.TTL == 0 {
				s.haveTXT, s.txt = false, nil
				return
			}
			s.txt, s.haveTXT = append([]string(nil), r.TXT...), true
		}
	case mdns.TypeA:
		h := c.host(r.Name)
		if r.TTL == 0 {
			delete(h.a, r.A)
		} else {
			h.a[r.A] = true
		}
	case mdns.TypeAAAA:
		h := c.host(r.Name)
		if r.TTL == 0 {
			delete(h.aaaa, r.AAAA)
		} else {
			h.aaaa[r.AAAA] = true
		}
	}
}

// foldMessage decodes nothing — Decode already ran — and applies a
// verified response's every record, all three sections: responders
// carry the SRV/TXT/A chain in additionals, and a browser that only
// read answers would print PTRs without resolution.
func (c *discCache) foldMessage(m *mdns.Message) {
	for _, sec := range [][]mdns.Record{m.Answers, m.Authorities, m.Additionals} {
		for _, r := range sec {
			c.foldRecord(r)
		}
	}
}

// foldDatagram validates and applies one slot-85 record: a 4-byte
// source-IPv4 prefix, the 8-byte UDP header, the mDNS message. Not
// every datagram on the port is our browse — a source port other than
// 5353 is not an mDNS responder, a non-response is not an answer, and a
// decode refusal is wire noise. Each counts ignored and is dropped;
// only a decoded response folds.
func (c *discCache) foldDatagram(d []byte) {
	if len(d) < 4+8 {
		c.ignored++
		return
	}
	srcPort := uint16(d[4])<<8 | uint16(d[5])
	if srcPort != mdns.Port {
		c.ignored++
		return
	}
	m, err := mdns.Decode(d[4+8:])
	if err != nil || !m.Response {
		c.ignored++
		return
	}
	c.replies++
	c.foldMessage(m)
}

// discBrowse is one bounded browse phase: every name's QU PTR query
// goes out at t=0 and again at discResendTick, and each of the
// discWindowTicks polls drains up to discDrainMax datagrams. Returns
// false on the first named send refusal (the caller exits).
func discBrowse(names []string, enc map[string][]byte, c *discCache, seam discSeam) bool {
	sendAll := func() bool {
		for _, name := range names {
			seam.log("netstat: discover query " + name)
			if rc := seam.send(mdns.Group4Bytes, mdns.Port, mdns.Port, enc[name]); rc < 0 {
				seam.log("netstat: discover send refused rc=" + vi.Itoa64(rc))
				return false
			}
		}
		return true
	}
	if !sendAll() {
		return false
	}
	buf := make([]byte, discRecvBuf)
	for t := 0; t < discWindowTicks; t++ {
		for i := 0; i < discDrainMax; i++ {
			n := seam.recv(mdns.Port, buf)
			if n <= 0 {
				break
			}
			c.foldDatagram(buf[:n])
		}
		if t == discResendTick {
			if !sendAll() {
				return false
			}
		}
		if t < discWindowTicks-1 {
			seam.sleep(1)
		}
	}
	return true
}

// discServiceLine renders one cache entry for the console: the resolved
// chain it has, honestly — an instance with no SRV yet prints
// "unresolved" rather than an invented address.
func discServiceLine(s *discSvc, c *discCache) string {
	line := "netstat: discover service " + s.name
	if !s.haveSRV {
		return line + " unresolved"
	}
	line += " host=" + s.target + " port=" + vi.Itoa64(int64(s.port))
	if h := c.hosts[foldName(s.target)]; h != nil {
		var addrs []string
		for a := range h.a {
			addrs = append(addrs, vi.FormatIPv4(a))
		}
		sort.Strings(addrs)
		for _, a := range addrs {
			line += " addr=" + a
		}
	}
	if s.haveTXT && len(s.txt) > 0 {
		line += " txt=" + strings.Join(s.txt, ";")
	}
	return line
}

// discPrint writes the deduped cache: type lines first (the bare
// verb's meta answers), then service lines — both sorted so the serial
// log is deterministic.
func discPrint(c *discCache, seam discSeam) {
	var types, svcs []*discSvc
	for _, s := range c.svcs {
		if s.kind == discKindType {
			types = append(types, s)
		} else {
			svcs = append(svcs, s)
		}
	}
	byName := func(a, b *discSvc) bool { return a.name < b.name }
	sort.Slice(types, func(i, j int) bool { return byName(types[i], types[j]) })
	sort.Slice(svcs, func(i, j int) bool { return byName(svcs[i], svcs[j]) })
	for _, s := range types {
		seam.log("netstat: discover type " + s.name)
	}
	for _, s := range svcs {
		seam.log(discServiceLine(s, c))
	}
}

// discEncode pre-encodes each name's one-question query. A name the
// codec cannot speak is refused by name before anything binds — the
// caller already validated it, so this is the belt.
func discEncode(names []string, seam discSeam) (map[string][]byte, bool) {
	enc := map[string][]byte{}
	for _, name := range names {
		pkt, err := mdns.Query(name, mdns.TypePTR, true).Encode()
		if err != nil {
			seam.log("netstat: discover encode refused " + name)
			return nil, false
		}
		enc[name] = pkt
	}
	return enc, true
}

// discNormName normalizes the service-type argument: `_workstation._tcp`
// browses `_workstation._tcp.local`; a name already carrying `.local`
// is used as-is. "" means the meta browse.
func discNormName(arg string) string {
	if arg == "" || strings.HasSuffix(strings.ToLower(arg), ".local") {
		return arg
	}
	return arg + ".local"
}

// runDiscover is the verb, end to end: named refusals before the bind,
// the bounded browse, the deduped print. Returns the exit status —
// 0 for a completed browse (services=0 is a result, not a failure),
// 1 for a refusal.
func runDiscover(args []string, seam discSeam) int {
	name := discMetaName
	if len(args) > 0 {
		name = discNormName(args[0])
		if !mdns.ValidName(name) {
			seam.log("netstat: discover usage: discover [service-type[.local]]")
			return 1
		}
	}
	snap, ok := seam.stats()
	if !ok || snap.OwnIP == [4]byte{} {
		seam.log("netstat: discover offline")
		return 1
	}
	if snap.OwnIP[0] == natSubnetByte0 && snap.OwnIP[1] == natSubnetByte1 && snap.OwnIP[2] == natSubnetByte2 {
		seam.log("netstat: discover needs bridged")
		return 1
	}
	if rc := seam.listen(mdns.Port); rc < 0 {
		seam.log("netstat: discover bind refused rc=" + vi.Itoa64(rc))
		return 1
	}
	defer func() { _ = seam.close(mdns.Port) }()

	seam.log("netstat: discover " + name)
	c := newDiscCache()
	names := []string{name}
	enc, okEnc := discEncode(names, seam)
	if !okEnc {
		return 1
	}
	if !discBrowse(names, enc, c, seam) {
		return 1 // discBrowse already logged the send refusal
	}
	if name == discMetaName {
		var types []string
		for _, s := range c.svcs {
			if s.kind == discKindType {
				types = append(types, s.name)
			}
		}
		sort.Strings(types)
		if len(types) > discMaxTypes {
			types = types[:discMaxTypes]
		}
		if len(types) > 0 {
			enc2, ok2 := discEncode(types, seam)
			if !ok2 || !discBrowse(types, enc2, c, seam) {
				return 1
			}
		}
	}
	discPrint(c, seam)
	var nTypes, nServices int
	for _, s := range c.svcs {
		if s.kind == discKindType {
			nTypes++
		} else {
			nServices++
		}
	}
	seam.log("netstat: discover done types=" + vi.Itoa64(int64(nTypes)) +
		" services=" + vi.Itoa64(int64(nServices)) +
		" replies=" + vi.Itoa64(int64(c.replies)) + " ignored=" + vi.Itoa64(int64(c.ignored)))
	return 0
}

// discoverMain is the `exec GONETSTAT.ELF discover [type]` entry — a
// pure console verb: no window opens, the browse prints and exits.
func discoverMain(args []string) {
	vi.Exit(runDiscover(args, guestDiscSeam()))
}
