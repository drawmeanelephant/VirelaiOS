package vi

import (
	"strconv"
	"strings"
	"testing"
)

func TestLevelLogFormatAndPlainParse(t *testing.T) {
	raw, rc := levelLogRow(nil, 'W', "net", "connected\nagain\rnow")
	if rc != 0 || raw != "1 W net connected again now" {
		t.Fatalf("row=%q rc=%d", raw, rc)
	}
	line := ParseLogLine(raw)
	if !line.Sequenced || line.Seq != 1 || line.Level != 'W' ||
		line.Tag != "net" || line.Message != "connected again now" || line.Raw != raw {
		t.Fatalf("parsed=%+v", line)
	}
	for _, raw := range []string{"started", "panic: fixture", "", "0 I net bad",
		"01 I net bad", "18446744073709551616 I net bad", "1 X net bad", "1 I"} {
		line := ParseLogLine(raw)
		if line.Sequenced || line.Level != 'I' || line.Tag != "" || line.Message != raw {
			t.Fatalf("legacy %q parsed=%+v", raw, line)
		}
	}
	raw, rc = levelLogRow(nil, 'I', "", "empty tag")
	if rc != 0 || raw != "1 I  empty tag" || ParseLogLine(raw).Tag != "" ||
		!ParseLogLine(raw).Sequenced {
		t.Fatalf("empty tag row=%q rc=%d", raw, rc)
	}
}

func TestLevelLogSequenceAcrossTrimAndPlainRows(t *testing.T) {
	var ring []byte
	for i := 1; i <= AppLogMaxLines*3; i++ {
		raw, rc := levelLogRow(ring, 'D', "ui", "row")
		if rc != 0 || ParseLogLine(raw).Seq != uint64(i) {
			t.Fatalf("seq %d row=%q rc=%d", i, raw, rc)
		}
		ring = trimLogRing(append(ring, []byte(raw+"\n")...), AppLogMaxLines, appLogMaxBytes)
	}
	rows := strings.Split(strings.TrimSuffix(string(ring), "\n"), "\n")
	if len(rows) != AppLogMaxLines || ParseLogLine(rows[0]).Seq != 65 {
		t.Fatalf("ring=%q", ring)
	}
	ring = append(ring, []byte("legacy\n")...)
	raw, rc := levelLogRow(ring, 'E', "net", "after plain")
	if rc != 0 || ParseLogLine(raw).Seq != 97 {
		t.Fatalf("continued=%q rc=%d", raw, rc)
	}
}

func TestLevelLogCapAndValidation(t *testing.T) {
	for _, tag := range []string{"", strings.Repeat("t", 32)} {
		raw, rc := levelLogRow([]byte("999 I net previous\n"), 'E', tag, strings.Repeat("x", 400))
		if rc != 0 || len(raw) != AppLogMaxLine || ParseLogLine(raw).Seq != 1000 {
			t.Fatalf("len=%d row=%q rc=%d", len(raw), raw, rc)
		}
	}
	for _, tag := range []string{"has space", "new\nline", strings.Repeat("x", 33), "é"} {
		if validLogTag(tag) || LogLevel("APP", 'I', tag, "message") != -ErrEINVAL {
			t.Fatalf("accepted tag %q", tag)
		}
	}
	if LogLevel("APP", 'X', "net", "message") != -ErrEINVAL {
		t.Fatal("accepted invalid level")
	}
	if _, rc := levelLogRow([]byte(strconv.FormatUint(^uint64(0), 10)+" I net last\n"),
		'I', "", "wrap"); rc != -ErrEINVAL {
		t.Fatalf("sequence wrap rc=%d", rc)
	}
}

func TestPlainLogAndReceiptBytesUnchanged(t *testing.T) {
	if got := oneLogLine("a\nb\rc"); got != "a b c" {
		t.Fatalf("normalization=%q", got)
	}
	if got := oneLogLine(strings.Repeat("x", 400)); len(got) != 256 {
		t.Fatalf("plain cap=%d", len(got))
	}
	var ring []byte
	for i := 0; i < 36; i++ {
		ring = trimLogRing(append(ring, []byte("line-"+twoDigits(i)+"\n")...),
			AppLogMaxLines, appLogMaxBytes)
	}
	var want string
	for i := 28; i < 36; i++ {
		want += "line-" + twoDigits(i) + "\n"
	}
	if got := string(lastLogLines(ring, crashLogLines)); got != want {
		t.Fatalf("last-log=%q want=%q", got, want)
	}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
