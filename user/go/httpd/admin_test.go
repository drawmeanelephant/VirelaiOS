package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"virelai/gpu3d"
	"virelai/vi"
)

// testSecret is the staged httpd-admin row value the stubs answer with.
const testSecret = "m209a-test-token"

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func goodAuth() string { return "Basic " + b64(adminUser+":"+testSecret) }

// storeWith answers the httpd-admin row and nothing else.
func storeWith(name string, out []byte) int {
	if name == adminKey {
		return copy(out, testSecret)
	}
	return -1
}

func storeEmpty(string, []byte) int { return -1 }

func namedProc(pid uint64, name string) vi.ProcRow {
	var r vi.ProcRow
	r.PID = pid
	r.State = vi.ProcRunning
	r.CpuTicks = 42
	r.MemBytes = 4096
	copy(r.NameBuf[:], name)
	return r
}

// adminSetup enables the section for one test and installs stub readers.
// Returns a restore-free environment via t.Cleanup.
func adminSetup(t *testing.T, secure bool, store func(string, []byte) int) {
	t.Helper()
	oldEnabled, oldSecure := adminEnabled, adminSecure
	oldGet := adminStoreGet
	oldProcs, oldNet := adminProcs, adminNetStats
	oldMounts := adminMounts
	oldGrants, oldDenials := adminGrants, adminDenials
	oldGPU, oldNow := adminGPU, adminNow
	t.Cleanup(func() {
		adminEnabled, adminSecure = oldEnabled, oldSecure
		adminStoreGet = oldGet
		adminProcs, adminNetStats = oldProcs, oldNet
		adminMounts = oldMounts
		adminGrants, adminDenials = oldGrants, oldDenials
		adminGPU, adminNow = oldGPU, oldNow
	})
	adminEnabled = true
	adminSecure = secure
	adminStoreGet = store
	adminNow = func() int64 { return 1700000000 }
	adminProcs = func() ([]vi.ProcRow, int64) {
		return []vi.ProcRow{namedProc(7, "GOHTTPD.ELF"), namedProc(3, "GOSH.ELF")}, 0
	}
	adminNetStats = func() (vi.NetStats, int64) {
		var s vi.NetStats
		s.OwnIP = [4]byte{10, 0, 0, 1}
		s.TXBytes = 1234
		return s, vi.NetStatsBytes
	}
	adminMounts = func() ([]vi.Mount, int64) {
		return []vi.Mount{{Name: "host", Kind: vi.MountKindShare, State: vi.MountMounted}}, 0
	}
	adminGrants = func() ([]vi.GrantRow, int64) {
		return []vi.GrantRow{{Name: "GOTOP.ELF", SHA: "*", Words: "net"}}, 0
	}
	adminDenials = func(uint64) ([]string, uint64, uint64, int64) {
		return []string{"RCPT stage=exec sha256=ab verdict=refused:net_denied name=GOIRC.ELF"}, 5, 0, 0
	}
	adminGPU = func() (gpu3d.ComputeStatsBlock, error) {
		return gpu3d.ComputeStatsBlock{Submits: 3, Completed: 3}, nil
	}
}

func adminReq(path, auth string) request {
	return request{Method: "GET", Path: path, Authorization: auth}
}

// noShare is a readFile that fails the test if the share fallback is
// reached for an /admin/ path, and answers 404-material otherwise.
func noShare(t *testing.T) func(string) []byte {
	return func(rel string) []byte {
		t.Errorf("share fallback reached for admin path: read %q", rel)
		return nil
	}
}

func TestAdminRefusesPlaintextByName(t *testing.T) {
	adminSetup(t, false, storeWith)
	got := route(adminReq("/admin/procs", goodAuth()), noShare(t))
	if got.Status != 403 {
		t.Fatalf("plaintext /admin/procs: status=%d, want 403", got.Status)
	}
	if !strings.Contains(string(got.Body), "admin requires tls") {
		t.Fatalf("plaintext body = %q, want the named refusal", got.Body)
	}
}

func TestAdminNoCredentialRowIs404(t *testing.T) {
	adminSetup(t, true, storeEmpty)
	for _, path := range []string{"/admin/", "/admin/procs", "/admin/gpu"} {
		got := route(adminReq(path, goodAuth()), noShare(t))
		if got.Status != 404 {
			t.Errorf("%s with no httpd-admin row: status=%d, want 404", path, got.Status)
		}
	}
}

func TestAdminWrongCredentials401(t *testing.T) {
	adminSetup(t, true, storeWith)
	for name, auth := range map[string]string{
		"no header":    "",
		"wrong secret": "Basic " + b64(adminUser+":nope"),
		"wrong user":   "Basic " + b64("root:"+testSecret),
		"not basic":    "Bearer " + b64(adminUser+":"+testSecret),
		"bad base64":   "Basic !!!",
		"empty pair":   "Basic " + b64(":"),
	} {
		got := route(adminReq("/admin/procs", auth), noShare(t))
		if got.Status != 401 {
			t.Errorf("%s: status=%d, want 401", name, got.Status)
			continue
		}
		var challenge bool
		for _, h := range got.Extra {
			if strings.HasPrefix(h, "WWW-Authenticate: Basic") {
				challenge = true
			}
		}
		if !challenge {
			t.Errorf("%s: 401 carried no Basic challenge: %v", name, got.Extra)
		}
	}
}

func TestAdminRoutesBeforeShareFallback(t *testing.T) {
	adminSetup(t, true, storeWith)
	// A share file at /host/admin/procs must never shadow the section.
	share := func(rel string) []byte {
		if rel == "/host/admin/procs" {
			return []byte("SHARE-BYTES")
		}
		t.Errorf("share fallback reached for admin path: %q", rel)
		return nil
	}
	got := route(adminReq("/admin/procs", goodAuth()), share)
	if got.Status != 200 || strings.Contains(string(got.Body), "SHARE-BYTES") {
		t.Fatalf("share shadowed the admin route: status=%d body=%.60q", got.Status, got.Body)
	}
	if !strings.Contains(string(got.Body), "GOHTTPD.ELF") {
		t.Fatalf("procs page missing the process row: %.200q", got.Body)
	}
}

func TestAdminDisabledFallsToShare(t *testing.T) {
	adminSetup(t, true, storeWith)
	adminEnabled = false
	share := func(rel string) []byte {
		if rel == "/host/admin/procs" {
			return []byte("SHARE-BYTES")
		}
		return nil
	}
	got := route(adminReq("/admin/procs", goodAuth()), share)
	if got.Status != 200 || !strings.Contains(string(got.Body), "SHARE-BYTES") {
		t.Fatalf("disabled: status=%d body=%q, want the share file", got.Status, got.Body)
	}
	got = route(adminReq("/admin/procs", ""), func(string) []byte { return nil })
	if got.Status != 404 {
		t.Fatalf("disabled, absent share file: status=%d, want 404", got.Status)
	}
}

func TestAdminUnknownPath404(t *testing.T) {
	adminSetup(t, true, storeWith)
	for _, path := range []string{"/admin/nope", "/admin/procs/x", "/admin/PROCS"} {
		got := route(adminReq(path, goodAuth()), noShare(t))
		if got.Status != 404 {
			t.Errorf("%s: status=%d, want 404", path, got.Status)
		}
	}
	// "/administrator" is not the section at all — it is a share path.
	var read []string
	got := route(adminReq("/administrator", ""), func(rel string) []byte {
		read = append(read, rel)
		return nil
	})
	if got.Status != 404 || len(read) != 1 || read[0] != "/host/administrator" {
		t.Fatalf("/administrator: status=%d reads=%v", got.Status, read)
	}
}

func TestAdminIndexAndPages(t *testing.T) {
	adminSetup(t, true, storeWith)
	for path, want := range map[string]string{
		"/admin/":       "/admin/procs",
		"/admin/procs":  "GOHTTPD.ELF",
		"/admin/net":    "10.0.0.1",
		"/admin/mounts": "host",
		"/admin/grants": "GOTOP.ELF",
		"/admin/gpu":    "submits",
	} {
		got := route(adminReq(path, goodAuth()), noShare(t))
		if got.Status != 200 || got.ContentType != "text/html; charset=utf-8" {
			t.Fatalf("%s: status=%d type=%q", path, got.Status, got.ContentType)
		}
		if !strings.Contains(string(got.Body), want) {
			t.Errorf("%s: body missing %q:\n%.400q", path, want, got.Body)
		}
		if !strings.Contains(string(got.Body), "sampled t=1700000000") {
			t.Errorf("%s: body missing the timestamp", path)
		}
	}
	if b := route(adminReq("/admin/grants", goodAuth()), noShare(t)).Body; !strings.Contains(
		string(b), "refused:net_denied") {
		t.Errorf("grants page missing the denial receipts: %.400q", b)
	}
}

func TestAdminRefusedReaderRendersUnknown(t *testing.T) {
	adminSetup(t, true, storeWith)
	adminGrants = func() ([]vi.GrantRow, int64) { return nil, -vi.ErrEACCES }
	adminProcs = func() ([]vi.ProcRow, int64) { return nil, -vi.ErrEACCES }
	adminGPU = func() (gpu3d.ComputeStatsBlock, error) {
		return gpu3d.ComputeStatsBlock{}, gpu3d.ErrComputeNoDevice
	}
	for path, want := range map[string]string{
		"/admin/grants": "unknown (EACCES)",
		"/admin/procs":  "unknown (EACCES)",
		"/admin/gpu":    "unknown (no-device)",
	} {
		got := route(adminReq(path, goodAuth()), noShare(t))
		if got.Status != 200 || !strings.Contains(string(got.Body), want) {
			t.Errorf("%s: status=%d body missing %q: %.300q", path, got.Status, want, got.Body)
		}
	}
}

func TestAdminCredentialNeverInBody(t *testing.T) {
	adminSetup(t, true, storeWith)
	for _, path := range []string{"/admin/", "/admin/procs", "/admin/net",
		"/admin/mounts", "/admin/grants", "/admin/gpu", "/admin/nope"} {
		got := route(adminReq(path, goodAuth()), noShare(t))
		if strings.Contains(string(got.Body), testSecret) || strings.Contains(string(got.Body), b64("admin:")) {
			t.Errorf("%s: credential material reached the body", path)
		}
	}
	// The 401 answer carries the challenge, never the expected secret.
	got := route(adminReq("/admin/procs", "Basic "+b64("admin:wrong")), noShare(t))
	if strings.Contains(string(got.Body), testSecret) {
		t.Error("401 body carried the stored credential")
	}
}

// TestSecretsTxtAnswerIsNotTheRouter pins the card's first-check answer at
// the layer this binary owns: the router treats /SECRETS.TXT as an
// ordinary share path and asks the file ABI for /host/SECRETS.TXT. On the
// guest that open is refused by name — the file is secret-class by
// construction (ADR 0024 D8, trust.ensure_secret_file armed before the
// store reads a byte) — so readFile comes back nil and the answer is 404.
// The live-httpd spec leg asserts the observed 404 on a boot whose share
// really carries SECRETS.TXT.
func TestSecretsTxtAnswerIsNotTheRouter(t *testing.T) {
	adminSetup(t, true, storeWith)
	var read []string
	got := route(adminReq("/SECRETS.TXT", goodAuth()), func(rel string) []byte {
		read = append(read, rel)
		return nil // the guest's FileOpen fails here: secret-class, every actor
	})
	if len(read) != 1 || read[0] != "/host/SECRETS.TXT" {
		t.Fatalf("/SECRETS.TXT did not reach the share fallback: %v", read)
	}
	if got.Status != 404 {
		t.Fatalf("/SECRETS.TXT with a refused open: status=%d, want 404", got.Status)
	}
}

func TestParseRequestCapturesAuthorization(t *testing.T) {
	req, ok := parseRequest([]byte(
		"GET /admin/procs HTTP/1.1\r\nAuthorization: Basic " + b64("admin:x") + "\r\n\r\n"))
	if !ok {
		t.Fatal("parseRequest refused a request carrying Authorization")
	}
	if req.Authorization != "Basic "+b64("admin:x") {
		t.Fatalf("Authorization = %q", req.Authorization)
	}
	req, ok = parseRequest([]byte("GET / HTTP/1.1\r\n\r\n"))
	if !ok || req.Authorization != "" {
		t.Fatalf("no header: ok=%v auth=%q", ok, req.Authorization)
	}
}
