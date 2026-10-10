package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These are host tests of the spec's actual Python assertion, not VZ evidence.
// The synthetic healthy transcript must pass; missing restarts, zero backoff,
// and missing receipt bytes must each fail that same assertion.
func TestGateAssertionRejectsBrokenEvidence(t *testing.T) {
	spec, err := os.ReadFile("../../../tools/gate/specs/live-supervise.spec")
	if err != nil {
		t.Fatal(err)
	}
	_, script, ok := strings.Cut(string(spec), "vgate_assert 01 python <<'PY'\n")
	if !ok {
		t.Fatal("missing gate assertion")
	}
	script, _, ok = strings.Cut(script, "\nPY\n")
	if !ok {
		t.Fatal("unterminated gate assertion")
	}
	// Share the spec's kill targets instead of duplicating its pinned PIDs.
	killTargets := regexp.MustCompile(`--send-text 'kill (\d+)'`).FindAllStringSubmatch(string(spec), -1)
	if len(killTargets) != 7 {
		t.Fatalf("want six restart kill targets and one never kill target, got %d", len(killTargets))
	}
	for _, mode := range []string{"healthy", "no-restarts", "no-backoff", "no-receipt"} {
		t.Run(mode, func(t *testing.T) {
			run := t.TempDir()
			share := filepath.Join(run, "share")
			if err := os.Mkdir(share, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(run, "artifacts"), 0700); err != nil {
				t.Fatal(err)
			}
			var serial strings.Builder
			now := int64(1e9)
			serial.WriteString(fmt.Sprintf("svc: start name=SVFIX-RESTART pid=%s t_ns=%d\n", killTargets[0][1], now))
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(run, path), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("client-launch.out", "svfixture: ready mode=restart n=1")
			for i, delay := range []int64{2, 4, 8, 8, 8} {
				serial.WriteString(fmt.Sprintf("svfixture: ready mode=restart n=%d\nkill: SVFIXCH.ELF armed\n", i+1))
				now += 1e9
				serial.WriteString(fmt.Sprintf("svc: exit name=SVFIX-RESTART status=137 t_ns=%d\n", now))
				serial.WriteString(fmt.Sprintf("svc: backoff name=SVFIX-RESTART k=%d delay_s=%d t_ns=%d\n", i+1, delay, now))
				if mode != "no-backoff" {
					now += delay * 1e9
				}
				serial.WriteString(fmt.Sprintf("svc: start name=SVFIX-RESTART pid=%s t_ns=%d\n", killTargets[i+1][1], now))
				write(fmt.Sprintf("client-kill%d.out", i+1), fmt.Sprintf("svc: backoff name=SVFIX-RESTART k=%d ", i+1))
			}
			serial.WriteString("svfixture: ready mode=restart n=6\nkill: SVFIXCH.ELF armed\n")
			now += 1e9
			serial.WriteString(fmt.Sprintf("svc: exit name=SVFIX-RESTART status=137 t_ns=%d\n", now))
			serial.WriteString(fmt.Sprintf("svc: failed name=SVFIX-RESTART reason=restart-limit t_ns=%d\n", now))
			serial.WriteString(fmt.Sprintf("svc: start name=SVFIX-NEVER pid=%s t_ns=%d\n", killTargets[6][1], now))
			serial.WriteString("svfixture: ready mode=never n=1\nkill: SVFIXNV.ELF armed\n")
			serial.WriteString(fmt.Sprintf("svc: exit name=SVFIX-NEVER status=137 t_ns=%d\n", now+1e9))
			serial.WriteString("svfixture: complete\n")
			write("client-kill6.out", "svc: failed name=SVFIX-RESTART reason=restart-limit ")
			write("client-never.out", "svfixture: complete")
			body := serial.String()
			if mode == "no-restarts" {
				body = body[:strings.Index(body, "svc: backoff")]
			}
			write("serial.log", body)
			if mode != "no-receipt" {
				write("share/SVFIX5.TXT", "app=SVFIXCH\noutcome=restart=5/5 status=137 backoff_s=8\nlast-log:\n")
			}
			cmd := exec.Command("python3", "-c", script)
			cmd.Dir = run
			cmd.Env = append(os.Environ(), "RUN_DIR="+run, "VG_SHARE="+share,
				"VG_SER="+filepath.Join(run, "serial.log"), "VIRELAI_GATE_SUFFIX=")
			out, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "healthy") {
				t.Fatalf("mode %s: err=%v\n%s", mode, err, out)
			}
		})
	}
}
