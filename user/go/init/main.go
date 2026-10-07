package main

import (
	"errors"

	"virelai/supervise"
	"virelai/svcmanifest"
	"virelai/vi"
)

func refusal(err error) string {
	var schema *svcmanifest.Error
	if errors.As(err, &schema) {
		return string(schema.Code)
	}
	return err.Error()
}

func main() {
	args := vi.Args()
	if len(args) != 3 {
		vi.ConsoleLine("init: refuse arguments")
		return
	}
	body, rc := vi.ReadFileAll(svcmanifest.Path, svcmanifest.MaxBytes+1)
	if rc < 0 {
		reason := "manifest-read"
		if rc == -vi.ErrENOENT {
			reason = "missing-manifest"
		}
		vi.ConsoleLine("init: refuse " + reason)
		return
	}
	manifest, err := svcmanifest.Parse(body)
	if err != nil {
		vi.ConsoleLine("init: refuse " + refusal(err))
		return
	}
	boot, err := NewBoot(manifest, args[1], args[2] == "sh", supervise.GuestHooks())
	if err != nil {
		vi.ConsoleLine("init: refuse " + refusal(err))
		return
	}
	deadline := vi.Nanos() + 60e9
	reload := newReloader(body, vi.ReadFileAll)
	acknowledged := false
	for {
		if boot.seated {
			reload.Poll(boot)
		}
		if err = boot.Tick(); err != nil {
			break
		}
		var message [64]byte
		if n, _ := vi.IpcRecv(message[:]); n > 0 && string(message[:n]) == "M92E:seated" {
			acknowledged = true
		}
		if acknowledged && boot.SeatReady() {
			boot.Seated()
		}
		if !boot.seated && vi.Nanos() >= deadline {
			err = errors.New("seat-timeout")
			break
		}
		vi.Sleep(1)
	}
	vi.ConsoleLine("init: refuse " + refusal(err))
	// Do not relinquish boot ownership while a partial start still owns
	// tasks. The monitor independently waits for init's final process exit.
	for {
		if stopErr := boot.Stop(); stopErr != nil {
			vi.ConsoleLine("init: stop failed " + stopErr.Error())
		}
		if tickErr := boot.Tick(); tickErr != nil {
			vi.ConsoleLine("init: cleanup poll failed")
		}
		if boot.Stopped() {
			return
		}
		vi.Sleep(1)
	}
}
