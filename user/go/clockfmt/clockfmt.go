// Package clockfmt is M83c (#1776): the one place that answers "what time is
// it HERE" for every Go consumer — GOSH's `date`, GOSET, and the apps. It
// owns the curated fixed-offset zone vocabulary and the shared local
// formatter; the `timezone` settings row stores one of its labels.
//
// Zones are FIXED OFFSETS, not political timezones: no IANA tzdata, no DST
// rule engine (named risk, not scope — a later card earns that). The honest
// model is exactly what the platform has: an epoch from the firmware (or the
// SNTP-corrected clock) and a user-chosen offset from it. The vocabulary is
// curated; Parse is lenient about the offset itself (a hand-edited
// `UTC+05:45` is a real fixed offset and formats correctly) but strict about
// the label's canonical shape.
//
// This package is pure: no vi, no syscalls, no stdlib time — the civil
// breakdown is done here so the conversion math is host-testable on its own.
package clockfmt

// Zone is a fixed offset from UTC in seconds east. UTC is the zero zone.
type Zone int32

// UTC is the zero offset and the fallback for every value that fails to
// parse: an invalid persisted row must never shift every clock silently.
const UTC Zone = 0

// Offsets is the curated cycle list GOSET offers (Left/Right on the
// `timezone` row). Order is west to east; the labels are the stored values.
var Offsets = []Zone{
	-8 * 3600, // UTC-08:00 — US Pacific standard
	-5 * 3600, // UTC-05:00 — US Eastern standard
	-3 * 3600, // UTC-03:00
	0, //         UTC
	1 * 3600, //  UTC+01:00 — Central Europe standard
	2 * 3600, //  UTC+02:00 — Eastern Europe standard
	3 * 3600, //  UTC+03:00
	5*3600 + 30*60, // UTC+05:30 — India
	8 * 3600, //  UTC+08:00
	9 * 3600, //  UTC+09:00
	10 * 3600, // UTC+10:00
	12 * 3600, // UTC+12:00
}

// maxOffset is the widest fixed offset any real zone uses (Kiribati is
// +14:00); anything wider is a typo, not a zone.
const maxOffset = 14 * 3600

// Label renders the canonical stored spelling of z: `UTC` at zero, else
// `UTC+HH:MM` / `UTC-HH:MM` with both fields zero-padded.
func Label(z Zone) string {
	if z == 0 {
		return "UTC"
	}
	off := int(z)
	sign := byte('+')
	if off < 0 {
		sign = '-'
		off = -off
	}
	b := [9]byte{'U', 'T', 'C', sign, '0', '0', ':', '0', '0'}
	b[4] += byte(off / 3600 / 10)
	b[5] += byte(off / 3600 % 10)
	b[7] += byte(off / 60 % 60 / 10)
	b[8] += byte(off / 60 % 60 % 10)
	return string(b[:])
}

// Names returns the curated labels, the vocabulary a `timezone` row cycles.
func Names() []string {
	out := make([]string, 0, len(Offsets))
	for _, z := range Offsets {
		out = append(out, Label(z))
	}
	return out
}

// Parse resolves a stored timezone value to its offset. It accepts `UTC` and
// the canonical `UTC±HH:MM` shape (zero-padded, |offset| <= 14:00); every
// other spelling fails. `UTC+00:00` is the zero zone, labelled `UTC`.
func Parse(name string) (Zone, bool) {
	if name == "UTC" {
		return UTC, true
	}
	if len(name) != 9 || name[0] != 'U' || name[1] != 'T' || name[2] != 'C' ||
		(name[3] != '+' && name[3] != '-') || name[6] != ':' {
		return 0, false
	}
	hh, ok := twoDigits(name[4], name[5])
	if !ok {
		return 0, false
	}
	mm, ok := twoDigits(name[7], name[8])
	if !ok || mm > 59 {
		return 0, false
	}
	off := Zone(hh*3600 + mm*60)
	if off > maxOffset {
		return 0, false
	}
	if name[3] == '-' {
		off = -off
	}
	return off, true
}

func twoDigits(a, b byte) (int, bool) {
	if a < '0' || a > '9' || b < '0' || b > '9' {
		return 0, false
	}
	return int(a-'0')*10 + int(b-'0'), true
}

// Civil is a broken-down calendar face in some zone.
type Civil struct {
	Year   int
	Month  int // 1..12
	Day    int // 1..31
	Hour   int // 0..23
	Minute int // 0..59
	Second int // 0..59
}

// At breaks the Unix epoch down in zone z: epoch + the zone's offset, read
// as a UTC calendar. Fixed offsets are just this addition — that is the
// whole reason DST is out of scope.
func At(epoch int64, z Zone) Civil {
	secs := epoch + int64(z)
	days := floorDiv(secs, 86400)
	rem := int(secs - days*86400)
	y, m, d := civilFromDays(days)
	return Civil{
		Year: y, Month: m, Day: d,
		Hour: rem / 3600, Minute: rem / 60 % 60, Second: rem % 60,
	}
}

// String renders the face as `2006-01-02 15:04:05` (the same calendar face
// `date` printed before this package existed).
func (c Civil) String() string {
	b := make([]byte, 0, 19)
	y := c.Year
	if y < 0 {
		b = append(b, '-')
		y = -y
	}
	b = appendDigits(b, y, 4)
	b = append(b, '-')
	b = appendDigits(b, c.Month, 2)
	b = append(b, '-')
	b = appendDigits(b, c.Day, 2)
	b = append(b, ' ')
	b = appendDigits(b, c.Hour, 2)
	b = append(b, ':')
	b = appendDigits(b, c.Minute, 2)
	b = append(b, ':')
	b = appendDigits(b, c.Second, 2)
	return string(b)
}

// Format renders epoch in zone z with the zone label: the one call `date`
// (and any app showing a timestamp) makes.
func Format(epoch int64, z Zone) string {
	return At(epoch, z).String() + " " + Label(z)
}

func appendDigits(b []byte, v, width int) []byte {
	for i := width - 1; i >= 0; i-- {
		d := v
		for j := 0; j < i; j++ {
			d /= 10
		}
		b = append(b, byte('0'+d%10))
	}
	return b
}

// floorDiv is integer division rounding toward negative infinity, so days
// before 1970 land on the right civil day.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// civilFromDays converts days since 1970-01-01 to a year/month/day face —
// Howard Hinnant's era-shifted algorithm, exact for every proleptic-Gregorian
// date the epoch can carry.
func civilFromDays(z int64) (year, month, day int) {
	z += 719468 // shift the era origin to 0000-03-01
	era := floorDiv(z, 146097)
	doe := z - era*146097 // [0, 146096]
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365 // [0, 399]
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100) // [0, 365]
	mp := (5*doy + 2) / 153                  // [0, 11]
	day = int(doy - (153*mp+2)/5 + 1)        // [1, 31]
	m := mp + 3                              // [3, 14]
	if mp >= 10 {
		m = mp - 9 // [1, 2] of the year after
	}
	year = int(y)
	if m <= 2 {
		year++
	}
	return year, int(m), day
}
