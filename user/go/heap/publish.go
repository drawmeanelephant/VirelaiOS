package heap

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"virelai/vi"
)

type Publisher struct {
	mu      sync.Mutex
	app     string
	pid     uint64
	session uint64
	seq     uint64
	ring    Ring
}

func NewPublisher(app string) (*Publisher, error) {
	if Path(app) == "" {
		return nil, fmt.Errorf("heap: invalid app label")
	}
	row, err := Resolve(app)
	if err != nil {
		return nil, err
	}
	session := uint64(vi.Nanos())
	if session == 0 {
		session = 1
	}
	return &Publisher{app: app, pid: row.PID, session: session}, nil
}

// Capture is also used by deterministic fixtures. Call at most once a second.
// Forced-GC time is separate from the kernel snapshot's 100 us budget.
func (publisher *Publisher) Capture() (Sample, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if publisher.seq == ^uint64(0) {
		return Sample{}, fmt.Errorf("heap: exhausted sequence")
	}
	start := vi.Nanos()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	gcNS := vi.Nanos() - start
	if publisher.seq == 0 {
		vi.ConsoleLine("heap: memstats ok")
	}
	sample := Sample{PID: publisher.pid, Session: publisher.session, Seq: publisher.seq + 1,
		LiveBytes: stats.HeapAlloc, Objects: stats.HeapObjects, Mallocs: stats.Mallocs,
		Frees: stats.Frees, NumGC: uint64(stats.NumGC)}
	if err := publisher.ring.Append(sample); err != nil {
		return Sample{}, err
	}
	publisher.seq = sample.Seq
	handle, rc := vi.FileOpen(Dir, vi.ModeWrite|vi.ModeCreate|vi.ModeDir)
	if rc >= 0 {
		vi.FileClose(uint32(handle))
	} else if rc != vi.ErrFileExists {
		return Sample{}, fmt.Errorf("heap: directory rc=%d", rc)
	}
	if rc := writeRing(Path(publisher.app), publisher.ring.Bytes(), vi.WriteFileSafe, time.Sleep); rc < 0 {
		return Sample{}, fmt.Errorf("heap: publish rc=%d", rc)
	}
	publishNS := vi.Nanos() - start
	vi.ConsoleLine(fmt.Sprintf("heap: published app=%s seq=%d live=%d gc_ns=%d publish_ns=%d",
		publisher.app, sample.Seq, sample.LiveBytes, gcNS, publishNS))
	return sample, nil
}

// A lost handle is retried from a fresh open, never continued at an assumed
// cursor. Only an observed successful whole-file publish counts as success.
// Three attempts bound both the work and a persistent underlying failure.
func writeRing(path string, body []byte, write func(string, []byte) int64, sleep func(time.Duration)) int64 {
	for attempt := 0; attempt < 3; attempt++ {
		rc := write(path, body)
		if rc != -vi.ErrEBADF || attempt == 2 {
			return rc
		}
		vi.ConsoleLine(fmt.Sprintf("heap: publish retry attempt=%d rc=%d", attempt+1, rc))
		sleep(time.Second)
	}
	return -vi.ErrEBADF
}

// Publish starts one opt-in goroutine, not another process. every is seconds;
// zero is clamped to one. Each sample's GC, ReadMemStats and file I/O complete
// before the next sleep starts, so this never busy-spins or catches up.
func Publish(app string, every uint64) {
	if every == 0 {
		every = 1
	}
	if every > uint64((1<<63-1)/int64(time.Second)) {
		every = uint64((1<<63 - 1) / int64(time.Second))
	}
	go func() {
		publisher, err := NewPublisher(app)
		if err == nil {
			for {
				if _, err = publisher.Capture(); err != nil {
					break
				}
				// Park the goroutine, not its entire Go M/P in a raw SVC.
				// The editor and GC must still run during this interval.
				time.Sleep(time.Duration(every) * time.Second)
			}
		}
		vi.ConsoleLine(fmt.Sprintf("heap: publisher error app=%s %v", app, err))
	}()
}
