package main

import (
	"strconv"
	"time"

	"virelai/vi"
)

// LOGFIX label rate count publishes rate rows per second. Each group
// is a burst; the final sleep keeps its runtime alive for the tasks probe.
func main() {
	args := vi.Args()
	if len(args) != 4 {
		vi.ConsoleLine("usage: LOGFIX label rate count")
		vi.Exit(2)
	}
	app := args[1]
	rate, e1 := strconv.Atoi(args[2])
	count, e2 := strconv.Atoi(args[3])
	if vi.AppLogPath(app) == "" || e1 != nil || e2 != nil ||
		rate < 1 || rate > 1024 || count < 1 || count > 4096 {
		vi.ConsoleLine("logfix: invalid arguments")
		vi.Exit(2)
	}
	for i := 1; i <= count; i++ {
		level, tag, message := fixtureRow(i)
		if rc := vi.LogLevel(app, level, tag, message); rc < 0 {
			vi.ConsoleLine("logfix: error app=" + app + " rc=" + vi.Itoa64(rc))
			vi.Exit(1)
		}
		if i == 1 {
			vi.ConsoleLine("logfix: active app=" + app)
		}
		if i%rate == 0 && i < count {
			time.Sleep(time.Second)
		}
	}
	vi.ConsoleLine("logfix: done app=" + app + " count=" + strconv.Itoa(count))
	time.Sleep(2 * time.Second)
}

func fixtureRow(i int) (byte, string, string) {
	tag := "net"
	if i%2 == 0 {
		tag = "ui"
	}
	n := strconv.Itoa(i)
	for len(n) < 6 {
		n = "0" + n
	}
	return "DIWE"[(i-1)%4], tag, "line=" + n
}
