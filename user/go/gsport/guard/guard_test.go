package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Each forbidden route gets a fixture; each must produce a violation.
func TestForbiddenFixtures(t *testing.T) {
	fixtures := map[string]string{
		"os/exec":   "package x\nimport \"os/exec\"\nvar _ = exec.Command\n",
		"os/signal": "package x\nimport \"os/signal\"\nvar _ = signal.Notify\n",
		"syscall":   "package x\nimport \"syscall\"\nvar _ = syscall.Kill\n",
		"x/sys/unix": "package x\n" +
			"import \"golang.org/x/sys/unix\"\nvar _ = unix.Socket\n",
		"unix-listen": "package x\n" +
			"import \"net\"\nvar _, _ = net.Listen(\"unix\", \"/tmp/s\")\n",
		"unix-listen-noarg": "package x\n" +
			"import \"net\"\nfunc f(n string) { _, _ = net.Listen(n, \"x\") }\n",
		"tcp-dial": "package x\n" +
			"import \"net\"\nvar _, _ = net.Dial(\"tcp\", \"127.0.0.1:1\")\n",
		"vi-call": "package x\n" +
			"import \"virelai/vi\"\nfunc f() { vi.ConsoleLine(\"x\") }\n",
		"vsys-call": "package x\n" +
			"import \"virelai/vsys\"\nfunc f() { vsys.Syscall0(1) }\n",
	}
	for name, src := range fixtures {
		v := CheckFile("x.go", []byte(src), false)
		if len(v) == 0 {
			t.Errorf("fixture %s produced no violation", name)
		}
	}
}

// Legal adapter source — gsport adapters call the abi seam, the mem://
// scheme is a string, net.Pipe is plumbing not transport.
func TestLegalAdapterSource(t *testing.T) {
	src := `package x

import (
	"net"

	"virelai/gsport/abi"
)

func f() error {
	_, _ = abi.FileOpen("/host/x", abi.ModeRead)
	a, b := net.Pipe()
	_ = a
	_ = b
	return nil
}
`
	if v := CheckFile("x.go", []byte(src), false); len(v) != 0 {
		t.Fatalf("legal source flagged: %v", v)
	}
}

// abi may import virelai/vi; nothing else may.
func TestABIExemption(t *testing.T) {
	src := `package abi

import "virelai/vi"

var open = vi.FileOpen
`
	if v := CheckFile("/repo/user/go/gsport/abi/x.go", []byte(src), abiImportOK("/repo/user/go/gsport/abi/x.go")); len(v) != 0 {
		t.Fatalf("abi source flagged: %v", v)
	}
	v := CheckFile("/repo/user/go/gsport/fsys/x.go", []byte(src), abiImportOK("/repo/user/go/gsport/fsys/x.go"))
	if len(v) == 0 {
		t.Fatal("vi import outside abi not flagged")
	}
}

// TestRealTreesClean walks the actual roots the card names — the adapter
// tree and every gostalgia overlay dir — and demands zero violations. This
// is the self-check the card wants: the guard fires on forbidden fixtures
// (above) and stays silent on the legal adapter code it protects.
func TestRealTreesClean(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Join(filepath.Dir(here), "..", "..", "..", "..")
	roots := []string{filepath.Join(repo, "user", "go", "gsport")}
	overlays, _ := filepath.Glob(filepath.Join(repo, "tools", "go", "overlay", "gostalgia*"))
	roots = append(roots, overlays...)
	var found []string
	for _, r := range roots {
		if _, err := os.Stat(r); err != nil {
			continue
		}
		found = append(found, r)
		for _, v := range CheckTree(r) {
			t.Errorf("violation: %s", v)
		}
	}
	if len(found) == 0 {
		t.Fatal("no guard roots found — expected at least user/go/gsport")
	}
}
