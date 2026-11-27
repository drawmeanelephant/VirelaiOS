// FLINTD.ELF --advertise NAME (M214c, #3920).
//
// The daemon publishes its FLINT-SYNC/1 endpoint as
// "NAME._flint._tcp.local" through the shared mdns.Advertiser — the same
// one-bind lifecycle GOHTTPD uses: a startup burst, a bounded query
// responder answering post-burst DNS-SD queries on the same :5353 owner,
// a steady TTL/2 re-announce, and a TTL=0 goodbye on clean exit.
//
// Staged ownership: a guest runs at most one mDNS advertiser at a time.
// HTTPD and FLINTD stage their lifecycles (the gate stops HTTPD before the
// daemon announces), so no guest carries two :5353 owners and INIT
// admission does not grow.
package main

import (
	"virelai/mdns"
	"virelai/vi"
)

const flintServiceType = "_flint._tcp.local"

// advSeam is the syscall surface the daemon's advertisement drives —
// injected so host tests exercise the service description and refusals
// without the guest.
type advSeam struct {
	stats func() (vi.NetStats, int64) // vi.NetStatsSnapshotRC
	log   func(line string)           // vi.ConsoleLine
	io    mdns.AdvSeam                // listen/send/recv/close/sleep (nil in unit tests)
}

func guestAdvSeam() advSeam {
	return advSeam{
		stats: vi.NetStatsSnapshotRC,
		log:   vi.ConsoleLine,
		io: mdns.AdvSeam{
			Listen: vi.UDPListen,
			Send:   vi.UDPSendFrom,
			Recv:   vi.UDPRecvFrom,
			Close:  vi.UDPClose,
			Sleep:  vi.Sleep,
		},
	}
}

// flintService is the DNS-SD description of the daemon's listener: the
// configured instance, the guest's own hub address, the served port and
// the link-local AAAA every guest carries.
func flintService(name string, port uint16, snap vi.NetStats) mdns.Service {
	return mdns.Service{
		Type:     flintServiceType,
		Instance: name,
		Host:     name + ".local",
		Port:     port,
		IPv4:     snap.OwnIP,
		IPv6:     mdns.LinkLocal6(snap.MAC),
		TTL:      mdns.AdvTTL,
		TXT:      []string{"txtvers=1", "proto=FLINT-SYNC/1"},
	}
}

// newAdv builds the advertisement or returns nil after its named console
// refusal. The checks mirror GOHTTPD's: a malformed label, a denied `net`
// grant and the VZ NAT attachment's no-LAN signature all refuse before
// anything binds.
func newAdv(name string, port uint16, seam advSeam) *mdns.Advertiser {
	if !mdns.ValidInstance(name) {
		seam.log("flintd: advertise bad name")
		return nil
	}
	snap, statrc := seam.stats()
	if statrc == -vi.ErrEACCES {
		seam.log("flintd: advertise needs net")
		return nil
	}
	if statrc != vi.NetStatsBytes || snap.OwnIP == [4]byte{} ||
		mdns.OnNatSubnet(snap.OwnIP) {
		seam.log("flintd: advertise needs bridged")
		return nil
	}
	advIO := seam.io
	advIO.Log = func(line string) { seam.log("flintd: " + line) }
	return mdns.NewAdvertiser(flintService(name, port, snap), advIO)
}
