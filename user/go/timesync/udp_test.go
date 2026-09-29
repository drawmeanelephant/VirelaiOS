package timesync

import (
	"errors"
	"testing"
	"unsafe"

	"virelai/vi"
)

// udpKernel is a fake for slots 9/10/11: it records the send and hands queued
// datagrams (8-byte UDP header + payload) back one per recv.
type udpKernel struct {
	listenRC, sendRC, recvRC int64
	listened                 []uint16
	sentIP                   [4]byte
	sentPort                 uint16
	sent                     []byte
	queue                    [][]byte
	recvs                    int
}

func (k *udpKernel) install(t *testing.T) {
	prev := vi.SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch num {
		case vi.SlotUDPListen:
			k.listened = append(k.listened, uint16(a0))
			return k.listenRC
		case vi.SlotUDPSend:
			if k.sendRC < 0 {
				return k.sendRC
			}
			k.sentIP = [4]byte{byte(a0 >> 24), byte(a0 >> 16), byte(a0 >> 8), byte(a0)}
			k.sentPort = uint16(a1)
			k.sent = append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(a2)), int(a3))...)
			return int64(a3)
		case vi.SlotUDPRecv:
			k.recvs++
			if k.recvRC < 0 {
				return k.recvRC
			}
			if len(k.queue) == 0 {
				return 0
			}
			d := k.queue[0]
			k.queue = k.queue[1:]
			n := copy(unsafe.Slice((*byte)(unsafe.Pointer(a1)), int(a2)), d)
			return int64(n)
		}
		return -vi.ErrENOSYS
	})
	t.Cleanup(func() { vi.SetSyscallHookForTest(prev) })
}

func dgram(srcPort uint16, payload []byte) []byte {
	h := []byte{byte(srcPort >> 8), byte(srcPort), 0x1b, 0x58, 0, byte(8 + len(payload)), 0, 0}
	return append(h, payload...)
}

func TestExchangeSendsOneRequestFromTheListeningPortAndReturnsTheMatch(t *testing.T) {
	k := &udpKernel{}
	k.install(t)
	req := BuildRequest(Timestamp{Sec: 11, Frac: 22})
	reply := goodServer(Timestamp{Sec: 11, Frac: 22}).bytes()
	k.queue = [][]byte{dgram(Port, reply)}

	got, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:], 50_000_000)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if string(got) != string(reply) {
		t.Fatalf("payload mismatch (the 8-byte header must be stripped)")
	}
	if len(k.listened) != 1 || k.listened[0] != 7000 {
		t.Fatalf("listened on %v, want the seam's source port 7000 before sending", k.listened)
	}
	if k.sentIP != [4]byte{10, 0, 0, 2} || k.sentPort != Port || string(k.sent) != string(req[:]) {
		t.Fatalf("sent %v:%d %d bytes", k.sentIP, k.sentPort, len(k.sent))
	}
}

func TestExchangeConsumesStraysAndKeepsWaitingForTheMatch(t *testing.T) {
	k := &udpKernel{}
	k.install(t)
	req := BuildRequest(Timestamp{Sec: 11, Frac: 22})
	good := goodServer(Timestamp{Sec: 11, Frac: 22}).bytes()
	k.queue = [][]byte{
		dgram(53, good), // a DNS-port datagram on the shared port
		dgram(Port, goodServer(Timestamp{Sec: 9}).bytes()), // a late answer to an earlier query
		dgram(Port, good[:20]),                             // a runt
		dgram(Port, good),                                  // ours
		dgram(Port, goodServer(Timestamp{Sec: 1}).bytes()), // never reached
	}
	got, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:], 50_000_000)
	if err != nil || string(got) != string(good) {
		t.Fatalf("err=%v match=%v", err, string(got) == string(good))
	}
	if len(k.queue) != 1 {
		t.Fatalf("%d datagrams left, want exactly the one after the match", len(k.queue))
	}
}

func TestExchangeGivesUpAtTheBudgetWithoutLoopingForever(t *testing.T) {
	k := &udpKernel{}
	k.install(t)
	req := BuildRequest(Timestamp{Sec: 1, Frac: 2})
	_, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:], 20_000_000) // 20 ms of real host time
	if !errors.Is(err, ErrNoReply) {
		t.Fatalf("err=%v, want ErrNoReply", err)
	}
	if k.recvs == 0 || k.recvs > maxPolls+1 {
		t.Fatalf("%d polls", k.recvs)
	}
	if len(k.sent) != PacketLen {
		t.Fatalf("sent %d bytes: the query goes out once and is not repeated", len(k.sent))
	}
}

func TestExchangeReportsTheSeamsRefusalsHonestly(t *testing.T) {
	req := BuildRequest(Timestamp{Sec: 1, Frac: 2})

	k := &udpKernel{sendRC: -vi.ErrEINVAL} // no_peer / not_ready: the server is not ARP-resolved
	k.install(t)
	if _, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:], 1); !errors.Is(err, ErrSendRefused) {
		t.Fatalf("send EINVAL: err=%v, want ErrSendRefused", err)
	}

	k = &udpKernel{sendRC: -5}
	k.install(t)
	var se sysErr
	if _, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:], 1); !errors.As(err, &se) || se != 5 {
		t.Fatalf("send errno 5: err=%v", err)
	}

	k = &udpKernel{recvRC: -vi.ErrEINVAL}
	k.install(t)
	if _, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:], 50_000_000); !errors.As(err, &se) || se != 1 {
		t.Fatalf("recv errno: err=%v", err)
	}

	// An already-bound port (EINVAL from listen: a DNS lookup bound it) is fine.
	k = &udpKernel{listenRC: -vi.ErrEINVAL}
	k.install(t)
	k.queue = [][]byte{dgram(Port, goodServer(Timestamp{Sec: 1, Frac: 2}).bytes())}
	if _, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:], 50_000_000); err != nil {
		t.Fatalf("listen EINVAL must be tolerated: %v", err)
	}

	if _, err := exchangeUDP([4]byte{10, 0, 0, 2}, req[:10], 1); !errors.Is(err, ErrShort) {
		t.Fatalf("a malformed request must never reach the wire: %v", err)
	}
}
