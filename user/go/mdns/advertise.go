// Shared mDNS advertiser (M214c #3920).
//
// GOHTTPD and FLINTD both publish one DNS-SD service over the same owned
// UDP :5353 seam: a startup announcement burst, a steady TTL/2
// re-announce, the bounded query responder from responder.go, and a TTL=0
// goodbye on Stop. This file is that lifecycle — extracted from HTTPD's
// M198b advertiser so the FLINT daemon reuses it instead of copying the
// HTTP protocol code. Each app supplies only its Service description and
// an already-prefixed log sink.
//
// One advertiser owns the port at a time: Start binds, Stop joins the burst
// and responder goroutines and closes the port, so staged lifecycles (HTTPD
// then FLINTD) never create two owners in one guest.
package mdns

import "virelai/vi"

const (
	// AdvTTL is the record TTL (RFC 6762 §10's recommended 120 s); the
	// steady re-announce fires at half of it.
	AdvTTL = 120
	// AdvBurstN is the startup announcement count, ~1 s apart (the
	// lost-packet cover RFC 6762 §8.3 asks for).
	AdvBurstN = 3
	// AdvBurstTick is one announcement interval in scheduler quanta
	// (~1 s at the 250 ms quantum).
	AdvBurstTick = 4
	// advHalfTicks is TTL/2 in scheduler quanta.
	advHalfTicks = AdvTTL / 2 * AdvBurstTick
)

// ValidInstance bounds a DNS-SD instance label to what the wire can carry
// and a browser can show: a non-empty DNS name, every label non-empty and
// ≤ 63 octets, total under the 255-octet name bound (appendName enforces
// the same bounds on encode — this check exists so a bad flag refuses by
// name before anything binds).
func ValidInstance(name string) bool {
	if name == "" || len(name) > MaxNameWire {
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

// LinkLocal6 derives a guest's link-local v6 address the way the kernel
// does: fe80:: plus the MAC's modified-EUI-64 IID (ipv6.iid_from_mac). The
// SLAAC global is kernel-internal with no EL0 read seam, so no routable v6
// is claimed where it cannot be observed.
func LinkLocal6(mac [6]byte) [16]byte {
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

// OnNatSubnet reports whether ip sits inside the VZ NAT attachment's
// observed 192.168.64.0/24 — the one attachment with no reachable LAN
// segment. --advertise refuses there by name instead of announcing into a
// segment no peer shares.
func OnNatSubnet(ip [4]byte) bool {
	return ip[0] == 192 && ip[1] == 168 && ip[2] == 64
}

// AdvSeam is the syscall surface the advertiser drives — injected so host
// tests run the whole announce/answer/goodbye lifecycle without the guest.
type AdvSeam struct {
	Listen func(port uint16) int64                                    // vi.UDPListen
	Send   func(dst [4]byte, dstPort, srcPort uint16, b []byte) int64 // vi.UDPSendFrom
	Recv   func(port uint16, buf []byte) int64                        // vi.UDPRecvFrom
	Close  func(port uint16) int64                                    // vi.UDPClose
	Sleep  func(ticks uint64)                                         // vi.Sleep
	Log    func(line string)                                          // already app-prefixed
}

// Advertiser is one advertisement's whole lifecycle. It is not safe for
// concurrent Start/Stop calls; the owning app drives it from one goroutine.
type Advertiser struct {
	svc       Service
	seam      AdvSeam
	announce  []byte
	goodbye   []byte
	responder *Responder
	pump      *Pump
	stopCh    chan struct{}
	burstDone chan struct{}
	loopDone  chan struct{}
	started   bool
}

// NewAdvertiser validates the service description and pre-encodes both the
// live announcement and the TTL=0 goodbye. A nil result is always preceded
// by its named log line — the caller exits.
func NewAdvertiser(svc Service, seam AdvSeam) *Advertiser {
	if seam.Log == nil {
		return nil
	}
	r, err := NewResponder(svc)
	if err != nil {
		seam.Log("advertise bad name")
		return nil
	}
	a := &Advertiser{svc: svc, seam: seam, responder: r}
	if a.announce, err = a.build(svc.TTL); err != nil {
		seam.Log("advertise encode failed")
		return nil
	}
	if a.goodbye, err = a.build(0); err != nil {
		seam.Log("advertise encode failed")
		return nil
	}
	return a
}

// Announce returns the pre-encoded live record set (tests and callers that
// need to inspect the wire shape).
func (a *Advertiser) Announce() []byte { return a.announce }

// Goodbye returns the pre-encoded TTL=0 record set.
func (a *Advertiser) Goodbye() []byte { return a.goodbye }

// Responder is the bounded matcher that answers post-burst queries on the
// same bind.
func (a *Advertiser) Responder() *Responder { return a.responder }

// ServiceName is the fully qualified instance label ("NAME._http._tcp.local").
func (a *Advertiser) ServiceName() string { return a.responder.serviceName() }

// BurstDone closes once the startup burst phase ends; a peer that browses
// only after it cannot be credited to the announcements. The console
// "advertise burst done" line is the corresponding serial boundary.
func (a *Advertiser) BurstDone() <-chan struct{} { return a.burstDone }

// build encodes the record set at ttl: the service PTR in the answer
// section with the instance's SRV+TXT, and the host's A/AAAA additionals.
func (a *Advertiser) build(ttl uint32) ([]byte, error) {
	svc := a.svc
	svc.TTL = ttl
	r, err := NewResponder(svc)
	if err != nil {
		return nil, err
	}
	recs, err := r.records()
	if err != nil {
		return nil, err
	}
	var answers, additionals []Record
	for _, rec := range recs {
		switch rec.Type {
		case TypeA, TypeAAAA:
			additionals = append(additionals, rec)
		default:
			answers = append(answers, rec)
		}
	}
	return (&Message{Response: true, Authoritative: true, Answers: answers, Additionals: additionals}).Encode()
}

// Start binds :5353, joins the shared query responder, emits the first
// announcement, and launches the burst/steady loop. A bind or send refusal
// is named, never silent — the caller exits rather than run unadvertised.
func (a *Advertiser) Start() bool {
	if rc := a.seam.Listen(Port); rc < 0 {
		if rc == -vi.ErrEACCES {
			a.seam.Log("advertise needs net-mdns")
		} else {
			a.seam.Log("advertise bind failed")
		}
		return false
	}
	if a.seam.Send == nil || a.seam.Recv == nil || a.seam.Sleep == nil ||
		a.seam.Close == nil {
		a.seam.Log("advertise responder failed")
		_ = a.seam.Close(Port)
		return false
	}
	a.stopCh = make(chan struct{})
	a.burstDone = make(chan struct{})
	a.loopDone = make(chan struct{})
	a.pump = a.responder.StartPump(PumpSeam{
		Recv:  a.seam.Recv,
		Send:  a.seam.Send,
		Sleep: a.seam.Sleep,
		Log:   a.seam.Log,
	})
	if a.pump == nil {
		a.seam.Log("advertise responder failed")
		_ = a.seam.Close(Port)
		return false
	}
	a.seam.Log("advertising " + a.ServiceName() + " port=" + vi.Itoa64(int64(a.svc.Port)))
	if !a.emit(a.announce) {
		a.pump.Stop()
		_ = a.seam.Close(Port)
		return false
	}
	a.started = true
	go a.runBurst()
	return true
}

// Stop signals and JOINS the burst and responder goroutines, sends the
// TTL=0 goodbye twice back-to-back (lost-packet cover), and releases the
// port so a staged next advertiser can bind immediately.
func (a *Advertiser) Stop() {
	if a == nil || !a.started {
		return
	}
	select {
	case <-a.stopCh:
	default:
		close(a.stopCh)
	}
	<-a.loopDone
	a.pump.Stop()
	a.started = false
	for i := 0; i < 2; i++ {
		if !a.emit(a.goodbye) {
			break
		}
	}
	a.seam.Log("advertise goodbye sent")
	_ = a.seam.Close(Port)
}

// wait parks up to ticks scheduler quanta, one at a time so Stop is observed
// promptly instead of after a whole TTL/2 interval.
func (a *Advertiser) wait(ticks uint64) bool {
	for i := uint64(0); i < ticks; i++ {
		select {
		case <-a.stopCh:
			return false
		default:
		}
		a.seam.Sleep(1)
	}
	return true
}

// runBurst is the startup burst then the steady TTL/2 re-announce.
// burstDone closes when the burst phase ends (the gate's post-burst
// boundary); loopDone closes only when Stop ends the steady loop.
func (a *Advertiser) runBurst() {
	defer close(a.loopDone)
	for i := 1; i < AdvBurstN; i++ {
		if !a.wait(AdvBurstTick) {
			close(a.burstDone)
			return
		}
		if !a.emit(a.announce) {
			close(a.burstDone)
			return
		}
	}
	a.seam.Log("advertise burst done")
	close(a.burstDone)
	for {
		if !a.wait(advHalfTicks) {
			return
		}
		if !a.emit(a.announce) {
			return
		}
	}
}

// emit sends one pre-encoded datagram to the mDNS group from the bound
// port; a refusal is logged with its errno, never dropped silently.
func (a *Advertiser) emit(msg []byte) bool {
	rc := a.seam.Send(Group4Bytes, Port, Port, msg)
	if rc < 0 {
		if rc == -vi.ErrEACCES {
			a.seam.Log("advertise needs net-mdns")
		} else {
			a.seam.Log("advertise send rc=" + vi.Itoa64(rc))
		}
		return false
	}
	a.seam.Log("advertise sent")
	return true
}
