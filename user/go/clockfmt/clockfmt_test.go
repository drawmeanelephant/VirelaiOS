package clockfmt

import "testing"

// TestAtBreaksDownTheEpoch pins the civil conversion against faces computed
// independently on the host (python datetime, 2026-09-29). The rows cross
// year, month, and day boundaries in both directions, include the 2024 leap
// day and the 1900/2100 century non-leap years, and reach the pre-1970
// negative epochs — the same math `date` runs in the guest.
func TestAtBreaksDownTheEpoch(t *testing.T) {
	cases := []struct {
		epoch int64
		zone  Zone
		want  string
	}{
		{0, UTC, "1970-01-01 00:00:00"},
		{0, 5 * 3600, "1970-01-01 05:00:00"},
		{0, -8 * 3600, "1969-12-31 16:00:00"},
		{-1, UTC, "1969-12-31 23:59:59"},
		{-1, 5*3600 + 30*60, "1970-01-01 05:29:59"},
		{1789043696, UTC, "2026-09-10 12:34:56"},
		{1789043696, 5*3600 + 30*60, "2026-09-10 18:04:56"},
		{1789043696, -8 * 3600, "2026-09-10 04:34:56"},
		{1789043696, 14 * 3600, "2026-09-11 02:34:56"}, // the day rolls over east
		{1709164800, UTC, "2024-02-29 00:00:00"},        // the leap day itself
		{1709164800, -8 * 3600, "2024-02-28 16:00:00"},  // ...and one day back west
		{4102444800, UTC, "2100-01-01 00:00:00"},        // 2100 is NOT a leap year
		{4102444800, -8 * 3600, "2099-12-31 16:00:00"},
		{-2208988800, UTC, "1900-01-01 00:00:00"}, // 1900 is NOT either
	}
	for _, c := range cases {
		if got := At(c.epoch, c.zone).String(); got != c.want {
			t.Errorf("At(%d, %d).String() = %q, want %q", c.epoch, c.zone, got, c.want)
		}
	}
}

// TestFormatCarriesTheZoneLabel pins the shared formatter's face: the civil
// face plus the canonical label, which is what `date` prints.
func TestFormatCarriesTheZoneLabel(t *testing.T) {
	cases := []struct {
		epoch int64
		zone  Zone
		want  string
	}{
		{1789043696, UTC, "2026-09-10 12:34:56 UTC"},
		{1789043696, 5*3600 + 30*60, "2026-09-10 18:04:56 UTC+05:30"},
		{1789043696, -8 * 3600, "2026-09-10 04:34:56 UTC-08:00"},
	}
	for _, c := range cases {
		if got := Format(c.epoch, c.zone); got != c.want {
			t.Errorf("Format(%d, %d) = %q, want %q", c.epoch, c.zone, got, c.want)
		}
	}
}

// TestParseAndLabelRoundTrip pins the stored vocabulary: every curated
// offset parses from its own label, the zero zone labels `UTC` under every
// spelling, and the shapes a typo produces are refused.
func TestParseAndLabelRoundTrip(t *testing.T) {
	for _, z := range Offsets {
		name := Label(z)
		got, ok := Parse(name)
		if !ok || got != z {
			t.Errorf("Parse(Label(%d)) = %d, %v; want %d, true", z, got, ok, z)
		}
	}
	if z, ok := Parse("UTC"); !ok || z != UTC {
		t.Errorf("Parse(UTC) = %d, %v; want 0, true", z, ok)
	}
	if z, ok := Parse("UTC+00:00"); !ok || z != UTC {
		t.Errorf("Parse(UTC+00:00) = %d, %v; want 0, true", z, ok)
	}
	if z, ok := Parse("UTC+05:45"); !ok || z != 5*3600+45*60 {
		t.Errorf("Parse(UTC+05:45) = %d, %v; want a real fixed offset", z, ok)
	}
	for _, bad := range []string{
		"", "utc", "UTC+", "UTC+5:30", "UTC+05:3", "UTC+0530",
		"UTC+05:60", "UTC+15:00", "UTC-15:00", "GMT", "Europe/Paris", "UTC+05:30 ",
	} {
		if z, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) = %d, true; want refusal", bad, z)
		}
	}
}

// TestLabelIsCanonical: the label is derived from the offset alone, so two
// spellings of one offset can never both be stored.
func TestLabelIsCanonical(t *testing.T) {
	if got := Label(0); got != "UTC" {
		t.Errorf("Label(0) = %q, want UTC", got)
	}
	if got := Label(5*3600 + 30*60); got != "UTC+05:30" {
		t.Errorf("Label(19800) = %q, want UTC+05:30", got)
	}
	if got := Label(-8 * 3600); got != "UTC-08:00" {
		t.Errorf("Label(-28800) = %q, want UTC-08:00", got)
	}
}

// TestNamesAreTheCuratedVocabulary pins the GOSET cycle list: the curated
// labels, west to east, `UTC` among them.
func TestNamesAreTheCuratedVocabulary(t *testing.T) {
	names := Names()
	if len(names) != len(Offsets) {
		t.Fatalf("Names() = %d names, want %d", len(names), len(Offsets))
	}
	seen := map[string]bool{}
	for i, n := range names {
		if seen[n] {
			t.Errorf("Names() repeats %q", n)
		}
		seen[n] = true
		if n != Label(Offsets[i]) {
			t.Errorf("Names()[%d] = %q, want %q", i, n, Label(Offsets[i]))
		}
	}
	if !seen["UTC"] {
		t.Errorf("Names() has no UTC: %v", names)
	}
}
