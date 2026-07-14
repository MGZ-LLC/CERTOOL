// Package ambient parses the AZ 88163 data-logger export and merges its
// temperature / humidity / pressure samples into a session by timestamp, so
// each record gets the ambient conditions that were logged at its capture time.
//
// The AZ export is a UTF-16LE, tab-delimited, CRLF text file (named .csv). It
// has a metadata header, a statistics block, a "Marked Events" section, then a
// data table:
//
//	Index  Date       Time       °C    %RH   hPa
//	1      14/7/2026  10:57:53   26.8  50.1  1017.3
//
// Sample timestamps are in the logger's own timezone (stated in the header, e.g.
// "Original time zone  UTC+1"); this parser converts them to UTC to line up with
// certool's UTC record timestamps.
package ambient

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Sample is one logged ambient reading at a UTC instant.
type Sample struct {
	Time     time.Time // UTC
	TempC    float64
	RH       float64
	Pressure float64 // hPa
}

// Log is a parsed AZ export: metadata plus the time-ordered samples.
type Log struct {
	Serial       string
	Company      string
	TZOffsetMin  int // minutes east of UTC, from the header
	SamplingText string
	Samples      []Sample
}

// ParseFile reads and parses an AZ 88163 export file.
func ParseFile(path string) (*Log, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse decodes the (UTF-16 or UTF-8) bytes and extracts the log.
func Parse(raw []byte) (*Log, error) {
	// BOMOverride switches to UTF-16 (LE/BE) when a BOM is present, else UTF-8.
	dec := unicode.BOMOverride(unicode.UTF8.NewDecoder())
	utf8Bytes, _, err := transform.Bytes(dec, raw)
	if err != nil {
		return nil, fmt.Errorf("ambient: decode: %w", err)
	}
	text := strings.ReplaceAll(string(utf8Bytes), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	log := &Log{TZOffsetMin: 0}
	loc := time.UTC
	inData := false

	for _, line := range lines {
		cols := splitTabs(line)
		if len(cols) == 0 {
			continue
		}
		key := cols[0]

		// Header metadata (labels may sit in either the left or right column).
		if !inData {
			if v := cellAfter(cols, "Company Name"); v != "" {
				log.Company = v
			}
			if v := cellAfter(cols, "SN"); v != "" && log.Serial == "" {
				log.Serial = v
			}
			if v := cellAfter(cols, "time zone"); v != "" {
				log.TZOffsetMin = parseTZ(v)
				loc = time.FixedZone("AZ", log.TZOffsetMin*60)
			}
			if v := cellAfter(cols, "Sampling Rate"); v != "" {
				log.SamplingText = v
			}
			// The data table header row is "Index Date Time ... °C ... %RH ... hPa".
			if strings.EqualFold(key, "Index") && rowHas(cols, "Date") && rowHas(cols, "Time") {
				inData = true
			}
			continue
		}

		// Data rows: after dropping the blank separator columns, expect
		// index, date, time, temp, rh, pressure.
		f := nonEmpty(cols)
		if len(f) < 6 {
			continue
		}
		if _, err := strconv.Atoi(f[0]); err != nil {
			continue // not a data row (index must be an integer)
		}
		ts, err := parseTimestamp(f[1], f[2], loc)
		if err != nil {
			continue
		}
		temp, e1 := strconv.ParseFloat(f[3], 64)
		rh, e2 := strconv.ParseFloat(f[4], 64)
		pres, e3 := strconv.ParseFloat(f[5], 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		log.Samples = append(log.Samples, Sample{Time: ts.UTC(), TempC: temp, RH: rh, Pressure: pres})
	}

	if len(log.Samples) == 0 {
		return nil, fmt.Errorf("ambient: no data rows parsed (unexpected format?)")
	}
	return log, nil
}

// NearestWithin returns the sample closest to t (UTC) within tol, ok=false if
// the closest is farther than tol.
func (l *Log) NearestWithin(t time.Time, tol time.Duration) (Sample, bool) {
	var best Sample
	bestGap := time.Duration(1<<62 - 1)
	for _, s := range l.Samples {
		gap := t.Sub(s.Time)
		if gap < 0 {
			gap = -gap
		}
		if gap < bestGap {
			bestGap, best = gap, s
		}
	}
	if bestGap > tol {
		return Sample{}, false
	}
	return best, true
}

// ---- helpers ----

func splitTabs(line string) []string {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return nil
	}
	parts := strings.Split(line, "\t")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func nonEmpty(cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// cellAfter finds a cell that equals (or contains) label and returns the next
// non-empty cell in the same row — handling the AZ's two-column header layout.
func cellAfter(cols []string, label string) string {
	l := strings.ToLower(label)
	for i, c := range cols {
		cl := strings.ToLower(strings.TrimSpace(c))
		if cl == l || strings.Contains(cl, l) {
			for j := i + 1; j < len(cols); j++ {
				if strings.TrimSpace(cols[j]) != "" {
					return strings.TrimSpace(cols[j])
				}
			}
		}
	}
	return ""
}

func rowHas(cols []string, want string) bool {
	for _, c := range cols {
		if strings.EqualFold(c, want) {
			return true
		}
	}
	return false
}

// parseTZ turns "UTC+1", "UTC-05", "UTC+5:30" into minutes east of UTC.
func parseTZ(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToUpper(s), "UTC")
	s = strings.TrimPrefix(s, "GMT")
	if s == "" {
		return 0
	}
	sign := 1
	if strings.HasPrefix(s, "-") {
		sign = -1
	}
	s = strings.TrimLeft(s, "+-")
	h, m := 0, 0
	if i := strings.IndexAny(s, ":."); i >= 0 {
		h, _ = strconv.Atoi(s[:i])
		m, _ = strconv.Atoi(strings.TrimLeft(s[i+1:], "0"))
		if s[i+1:] == "30" { // e.g. UTC+5:30
			m = 30
		}
	} else {
		h, _ = strconv.Atoi(s)
	}
	return sign * (h*60 + m)
}

// parseTimestamp parses AZ date "14/7/2026" (D/M/YYYY) + time "10:57:53" in loc.
// The AZ export leaves hour/minute/second UN-padded (e.g. "11:2:53"), so the
// time is normalised to two-digit fields before parsing.
func parseTimestamp(date, tm string, loc *time.Location) (time.Time, error) {
	tm = padTime(tm)
	for _, layout := range []string{"2/1/2006 15:04:05", "2/1/2006 15:04"} {
		if t, err := time.ParseInLocation(layout, date+" "+tm, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("ambient: bad timestamp %q %q", date, tm)
}

// padTime zero-pads each ':'-separated component of a time string to 2 digits.
func padTime(tm string) string {
	parts := strings.Split(tm, ":")
	for i := range parts {
		if len(parts[i]) == 1 {
			parts[i] = "0" + parts[i]
		}
	}
	return strings.Join(parts, ":")
}
