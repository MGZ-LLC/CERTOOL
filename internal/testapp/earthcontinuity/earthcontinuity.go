// Package earthcontinuity is the reference test app: protective-bonding
// continuity (earth continuity) per EN 60204-1:2018+A1:2025 Cl. 18.1(b)/18.2.2,
// following the standard four-wire (Kelvin) method.
//
// It demonstrates every framework hook: setup inputs (the bonding-point list),
// Prepare() building a run sheet with the expected resistance prefilled from
// conductor geometry, Evaluate() computing R = V/I and applying the acceptance
// rule, and Report() rendering the Obsidian report body.
package earthcontinuity

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MGZ-LLC/CERTOOL/internal/model"
	"github.com/MGZ-LLC/CERTOOL/internal/testapp"
	"github.com/MGZ-LLC/CERTOOL/internal/version"
)

// copperRhoMilliOhmMetre is ρ for copper at 20 °C expressed so that
// mΩ/m = copperRhoMilliOhmMetre / CSA(mm²). ρ = 0.0172 Ω·mm²/m ⇒ 17.2 mΩ·mm²/m.
const copperRhoMilliOhmMetre = 17.2

// jointAllowanceMilliOhm is the allowance per crimped/bolted joint in good
// condition (EN 60204-1 Cl. 18).
const jointAllowanceMilliOhm = 5.0

type app struct{}

func init() { testapp.Register(app{}) }

func (app) Info() testapp.Info {
	return testapp.Info{
		ID:          "earth-continuity",
		Name:        "Protective Bonding Continuity",
		Standard:    "EN 60204-1:2018+A1:2025, Cl. 18.1(b), 18.2.2",
		Description: "Four-wire (Kelvin) bonding-continuity measurement from the central bonding point to each bonded destination. Floating CC source; R = V / I.",
	}
}

// SetupFields: the app-specific inputs. The bonding points are given as a
// multi-line list so the run sheet and expected values are generated from the
// frozen wiring record.
func (app) SetupFields() []model.FieldSpec {
	return []model.FieldSpec{
		{
			Key: "main_current", Label: "Main-path test current", Type: model.FieldNumber,
			Source: model.SourceManual, Unit: "A", Expected: model.Num(3),
			Help: "EN 60204-1 Cl. 18.2.2 nominal 10 A, but the Keysight E36312A CH1 tops out at 5 A; " +
				"a typical bonding run uses ~3 A. Keep ≤ 5 A on CH1.",
		},
		{
			Key: "small_current", Label: "Small-bond test current", Type: model.FieldNumber,
			Source: model.SourceManual, Unit: "A", Expected: model.Num(1),
			Help: "EN 60204-1 Cl. 18.2.2: 1 A for small functional/shield bonds.",
		},
		{
			Key: "vlimit", Label: "Source voltage limit", Type: model.FieldNumber,
			Source: model.SourceManual, Unit: "V", Expected: model.Num(6),
			Help: "≤ 24 V (Cl. 18.2.2); 6 V is ample for mΩ paths.",
		},
		{
			Key: "channel", Label: "Source output channel", Type: model.FieldNumber,
			Source: model.SourceManual, Expected: model.Num(1),
			Help: "Keysight E36312A: CH1 = 6 V / 5 A (high current — use this); CH2/CH3 = 25 V / 1 A.",
		},
		{
			Key: "points", Label: "Bonding points", Type: model.FieldText, Source: model.SourceManual,
			Help: "One point per line:  ID | title | csa_mm2 | length_m | joints | small?\n" +
				"AUTO-FILL: leave the ID blank (start the line with |) to auto-number B-01, B-02, …\n" +
				"           leave csa/length/joints/small blank to REPEAT the previous row (set a preset once).\n" +
				"e.g.   | CBP → DIN rail | 4 | 0.35 | 2 | no      then   | CBP → panel      (repeats 4/0.35/2/no)\n" +
				"Leave csa+length blank entirely for smart manual entry (no computed expectation).",
			ExpectedText: defaultPoints,
		},
	}
}

// point is one parsed bonding destination.
type point struct {
	id     string
	title  string
	csa    float64 // mm², 0 = unknown
	length float64 // m, 0 = unknown
	joints int
	small  bool
}

// parsePoints reads the bonding-point list with two auto-fill conveniences:
//   - a blank ID (start the line with "|") is auto-numbered B-01, B-02, …
//   - a blank CSA/length/joints/small repeats the value from the previous row
//     (so you set a preset once and only override where it changes).
func parsePoints(raw string) []point {
	var pts []point
	seq := 0
	var last point
	haveLast := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "|")
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		get := func(i int) string {
			if i < len(cols) {
				return cols[i]
			}
			return ""
		}

		p := point{}
		if id := get(0); id != "" {
			p.id = id
			if n := trailingInt(id); n > seq {
				seq = n // keep auto-numbering after an explicit B-05
			}
		} else {
			seq++
			p.id = fmt.Sprintf("B-%02d", seq)
		}
		p.title = get(1) // titles are per-point, never inherited

		if v := get(2); v != "" {
			p.csa, _ = strconv.ParseFloat(v, 64)
		} else if haveLast {
			p.csa = last.csa
		}
		if v := get(3); v != "" {
			p.length, _ = strconv.ParseFloat(v, 64)
		} else if haveLast {
			p.length = last.length
		}
		if v := get(4); v != "" {
			p.joints, _ = strconv.Atoi(v)
		} else if haveLast {
			p.joints = last.joints
		}
		if v := get(5); v != "" {
			p.small = isSmall(v)
		} else if haveLast {
			p.small = last.small
		}

		pts = append(pts, p)
		last, haveLast = p, true
	}
	return pts
}

func isSmall(s string) bool {
	s = strings.ToLower(s)
	return s == "yes" || s == "y" || s == "true" || s == "small"
}

// trailingInt returns the trailing integer of e.g. "B-05" -> 5, or 0.
func trailingInt(s string) int {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return 0
	}
	n, _ := strconv.Atoi(s[i:])
	return n
}

// expectedMilliOhm returns the computed expectation and ok=false if geometry is
// unknown (→ smart manual entry).
func expectedMilliOhm(p point) (float64, bool) {
	if p.csa <= 0 || p.length <= 0 {
		return 0, false
	}
	r := (copperRhoMilliOhmMetre/p.csa)*p.length + float64(p.joints)*jointAllowanceMilliOhm
	return r, true
}

func (a app) Prepare(s *model.Session, params map[string]string) ([]model.RecordSpec, error) {
	mainI := paramFloat(params, "main_current", 3)
	smallI := paramFloat(params, "small_current", 1)
	vlim := paramFloat(params, "vlimit", 6)
	ch := int(paramFloat(params, "channel", 1))

	// Stash the settings so points added mid-run (NewPoint) reuse them.
	if s.Meta == nil {
		s.Meta = map[string]string{}
	}
	s.Meta["main_current"] = strconv.FormatFloat(mainI, 'f', -1, 64)
	s.Meta["small_current"] = strconv.FormatFloat(smallI, 'f', -1, 64)
	s.Meta["vlimit"] = strconv.FormatFloat(vlim, 'f', -1, 64)
	s.Meta["channel"] = strconv.Itoa(ch)

	raw := params["points"]
	if strings.TrimSpace(raw) == "" {
		raw = defaultPoints
	}
	pts := parsePoints(raw)
	if len(pts) == 0 {
		return nil, fmt.Errorf("earth-continuity: no bonding points defined")
	}

	specs := make([]model.RecordSpec, 0, len(pts)+1)
	// Opening marker: ambient conditions + overall photo, timestamped so bench
	// photos correlate.
	specs = append(specs, ambientMarker())
	for _, p := range pts {
		specs = append(specs, buildPointSpec(p, mainI, smallI, vlim, ch))
	}
	return specs, nil
}

// buildPointSpec turns one bonding point into a record spec, prefilling the
// expected resistance and the instrument settings. Shared by Prepare and
// NewPoint so upfront and mid-run points are identical.
func buildPointSpec(p point, mainI, smallI, vlim float64, ch int) model.RecordSpec {
	testI := mainI
	if p.small {
		testI = smallI
	}
	if ch < 1 {
		ch = 1
	}
	exp, haveExp := expectedMilliOhm(p)

	fields := []model.FieldSpec{
		{Key: "I", Label: "Test current I", Type: model.FieldNumber, Source: model.SourceDevice,
			Unit: "A", DeviceChannel: "I", Expected: model.Num(testI), Required: true,
			Help: "Read back from the source; should match the set current."},
		{Key: "V", Label: "Voltage drop V", Type: model.FieldNumber, Source: model.SourceDevice,
			Unit: "mV", DeviceChannel: "V", Required: true,
			Help: "Four-wire sense across the two points, inboard of the current clips."},
		{Key: "R", Label: "Resistance R = V/I", Type: model.FieldNumber, Source: model.SourceComputed,
			Unit: "mΩ", ReadOnly: true},
		{Key: "stable", Label: "Stable while flexing?", Type: model.FieldBool, Source: model.SourceManual,
			Required: true, Help: "EN 60204-1 Cl. 18: reading must stay stable while gently flexing the joint."},
		{Key: "conductor", Label: "Conductor (CSA × length)", Type: model.FieldText, Source: model.SourceManual},
		{Key: "note", Label: "Observation", Type: model.FieldText, Source: model.SourceManual},
	}
	if haveExp {
		fields[2].Expected = model.Num(exp)
	}
	if p.csa > 0 && p.length > 0 {
		fields[4].ExpectedText = fmt.Sprintf("%.3g mm² × %.3g m, %d joint(s)", p.csa, p.length, p.joints)
	}
	return model.RecordSpec{
		ID:    p.id,
		Title: p.title,
		Fields: fields,
		DeviceSettings: map[string]string{
			"channel": strconv.Itoa(ch),
			"current": strconv.FormatFloat(testI, 'f', -1, 64),
			"vlimit":  strconv.FormatFloat(vlim, 'f', -1, 64),
			"output":  "on",
		},
		Note: expectationNote(exp, haveExp),
	}
}

// PointFields defines the inputs to add one bonding point mid-run.
func (app) PointFields() []model.FieldSpec {
	return []model.FieldSpec{
		{Key: "id", Label: "Point ID", Type: model.FieldText, Source: model.SourceManual, Required: true,
			Help: "e.g. B-08"},
		{Key: "title", Label: "From → To", Type: model.FieldText, Source: model.SourceManual, Required: true,
			Help: "e.g. CBP → extra shield bond"},
		{Key: "csa", Label: "Conductor CSA", Type: model.FieldNumber, Source: model.SourceManual, Unit: "mm²",
			Help: "blank → smart manual entry (no computed expectation)"},
		{Key: "length", Label: "Conductor length", Type: model.FieldNumber, Source: model.SourceManual, Unit: "m"},
		{Key: "joints", Label: "Joints", Type: model.FieldNumber, Source: model.SourceManual},
		{Key: "small", Label: "Small/shield bond (use small current)?", Type: model.FieldBool, Source: model.SourceManual},
	}
}

// NewPoint builds one bonding-point spec from PointFields values, reusing the
// currents chosen at setup.
func (app) NewPoint(s *model.Session, params map[string]string) (model.RecordSpec, error) {
	id := strings.TrimSpace(params["id"])
	if id == "" {
		return model.RecordSpec{}, fmt.Errorf("earth-continuity: point ID required")
	}
	p := point{
		id:    id,
		title: strings.TrimSpace(params["title"]),
		small: isTrue(params["small"]),
	}
	p.csa, _ = strconv.ParseFloat(strings.TrimSpace(params["csa"]), 64)
	p.length, _ = strconv.ParseFloat(strings.TrimSpace(params["length"]), 64)
	p.joints, _ = strconv.Atoi(strings.TrimSpace(params["joints"]))

	mainI := paramFloat(s.Meta, "main_current", 3)
	smallI := paramFloat(s.Meta, "small_current", 1)
	vlim := paramFloat(s.Meta, "vlimit", 6)
	ch := int(paramFloat(s.Meta, "channel", 1))
	return buildPointSpec(p, mainI, smallI, vlim, ch), nil
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "1", "true", "yes", "y":
		return true
	}
	return false
}

func expectationNote(exp float64, ok bool) string {
	if !ok {
		return "Expected value unknown — smart manual entry; record measured R and judge against EN 60204-1 Cl. 18."
	}
	return fmt.Sprintf("Expected ≈ %.1f mΩ; pass ≤ %.1f mΩ (expected +50%%) and stable; investigate > 100 mΩ.", exp, exp*1.5)
}

func ambientMarker() model.RecordSpec {
	return model.RecordSpec{
		ID:     "ENV-00",
		Title:  "Start ambient logger & baseline",
		Marker: true,
		Note: "Manual operation — AZ 88163 logger: press START for 5 s until REC flashes, " +
			"confirm the sampling interval, and place it at the bench. It records T/RH/Baro on " +
			"its own clock; bench records here correlate to it by timestamp. Take the overall setup " +
			"photo now. Enter a baseline reading below (a fallback if the logger export is not merged).",
		Fields: []model.FieldSpec{
			{Key: "logger_serial", Label: "AZ 88163 serial", Type: model.FieldText, Source: model.SourceManual,
				Help: "For the equipment record."},
			{Key: "sampling_s", Label: "Logger sampling interval", Type: model.FieldNumber, Source: model.SourceManual, Unit: "s",
				Expected: model.Num(30), Help: "30 s recommended for a bench session."},
			{Key: "logger_started", Label: "Logger started (REC flashing)?", Type: model.FieldBool, Source: model.SourceManual,
				Required: true},
			{Key: "temp_c", Label: "Baseline temperature", Type: model.FieldNumber, Source: model.SourceManual, Unit: "°C"},
			{Key: "humidity", Label: "Baseline humidity", Type: model.FieldNumber, Source: model.SourceManual, Unit: "%"},
			{Key: "pressure", Label: "Baseline pressure", Type: model.FieldNumber, Source: model.SourceManual, Unit: "hPa"},
			{Key: "overall_photo", Label: "Overall setup photo taken?", Type: model.FieldBool, Source: model.SourceManual,
				Help: "Its timestamp will match this record."},
			{Key: "note", Label: "Note", Type: model.FieldText, Source: model.SourceManual},
		},
	}
}

// FinishInstructions surfaces the AZ 88163 manual close-out on the finish page.
func (app) FinishInstructions() []string {
	return []string{
		"Stop the AZ 88163 logger: press STOP for 5 s (or plug it into the PC — that also stops it).",
		"Run the on-device \"PDF Logger Configuration Tool\" → Convert to Excel to export the tab-delimited log.",
		"Save the export into this session's evidence/instrument-logs/ folder (e.g. AZ88163_<session>.csv).",
		"Ambient at each record's timestamp can then be read from that file (its clock is PC-local — mind the UTC offset).",
	}
}

// Evaluate computes R = V/I and applies the acceptance rule (EN 60204-1 Cl. 18).
func (app) Evaluate(spec model.RecordSpec, rec *model.Record) error {
	if spec.Marker {
		rec.Verdict = model.VerdictInfo
		return nil
	}
	iv, iok := numValue(rec, "I")
	vv, vok := numValue(rec, "V")
	if iok && vok && iv != 0 {
		r := vv / iv // mV / A = mΩ
		rec.Values["R"] = model.Value{Key: "R", Num: model.Num(r), Source: model.SourceComputed}
	}

	rv, rok := numValue(rec, "R")
	stable, sok := boolValue(rec, "stable")
	setI, haveSetI := specExpected(spec, "I")
	exp, haveExp := specExpected(spec, "R")

	// Every fail condition is tested before the 100 mΩ investigate ceiling, so a
	// failed path can never be reported as merely "investigate" — or as a pass.
	rec.VerdictNote = ""
	switch {
	case iok && vok && (iv <= 0 || (haveSetI && setI > 0 && iv < openCircuitFraction*setI)):
		// A constant-current source that cannot drive its set current has hit its
		// voltage limit: the path is open or grossly resistive. V/I is then not a
		// bond resistance — a near-zero I once turned an open circuit into a
		// huge negative R that passed the expected+50 % comparison.
		rec.Verdict = model.VerdictFail
		drive := fmt.Sprintf("measured I %g A", iv)
		if haveSetI && setI > 0 {
			drive += fmt.Sprintf(" against %g A set", setI)
		}
		rec.VerdictNote = "Open circuit or gross resistance — " + drive + "; the source did not drive the test current. Re-seat the clips and re-measure (EN 60204-1 Cl. 18.2.2)."
	case !rok:
		rec.Verdict = model.VerdictPending
	case rv <= 0:
		rec.Verdict = model.VerdictFail
		rec.VerdictNote = "R ≤ 0 is not a physical reading — check sense-lead polarity and connections, then re-measure."
	case sok && !stable:
		rec.Verdict = model.VerdictFail
		rec.VerdictNote = "Reading unstable while flexing — bad bond (EN 60204-1 Cl. 18)."
	case haveExp && rv > exp*1.5:
		rec.Verdict = model.VerdictFail
		rec.VerdictNote = fmt.Sprintf("R %.1f mΩ exceeds expected+50%% (%.1f mΩ).", rv, exp*1.5)
	case rv > 100:
		rec.Verdict = model.VerdictInvestigate
		rec.VerdictNote = "R > 100 mΩ — investigate (EN 60204-1 Cl. 18)."
	case haveExp:
		rec.Verdict = model.VerdictPass
	default:
		// Without an expectation the +50 % rule cannot be applied, so the record is
		// neither a pass nor a fail and must not count as "within acceptance".
		rec.Verdict = model.VerdictNotAssessed
		rec.VerdictNote = "No expected value (conductor CSA × length not given) — not assessed against expected +50 %."
	}
	return nil
}

// openCircuitFraction is the share of the set test current below which a reading
// is treated as an open circuit: a source in constant-current regulation reads
// back its set current to well within this margin.
const openCircuitFraction = 0.9

func (a app) Report(s *model.Session) (string, error) {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# Test Report — %s — Protective Bonding Continuity\n\n", nz(s.Product, "____"))
	w("**Method:** four-wire (Kelvin) bonding continuity, EN 60204-1 Cl. 18.2.2\n")
	w("**Standard:** %s\n", a.Info().Standard)
	w("**Test date:** %s\n", s.StartedAt.Format("2006-01-02"))
	w("**Location:** %s\n", nz(s.Location, "____"))
	w("**Tester:** %s   **Witness:** %s\n", nz(s.Operator, "____"), nz(s.Witness, "____"))
	w("**Connector:** %s\n", s.Connector)
	w("**Recorded with:** %s\n", version.String())
	w("**Report status:** ☑ draft · ☐ complete · ☐ reviewed\n\n---\n\n")

	w("## 1. Equipment under test (EUT)\n\n")
	w("| Field | Value |\n|---|---|\n")
	w("| Product / version | %s |\n", nz(s.Product, ""))
	w("| Serial number | %s |\n", nz(s.Serial, ""))
	w("| Released configuration reference | %s |\n", nz(s.ConfigRef, ""))
	w("| Wiring record revision (frozen) | %s |\n\n", nz(s.WiringRef, ""))

	w("> **Source check:** machine 24 V PSU **disconnected**; floating CC source (Cl. 18.2.2 — earthed PELV must not be the source).\n\n")

	w("## 2. Test equipment\n\n")
	if len(s.Instruments) == 0 {
		w("_No instruments auto-captured (manual entry)._\n\n")
	} else {
		w("| Role | Manufacturer | Model | Serial | Firmware | Calibration due |\n")
		w("|---|---|---|---|---|---|\n")
		for _, in := range s.Instruments {
			w("| %s | %s | %s | %s | %s | %s |\n",
				nz(in.Role, "—"), nz(in.Manufacturer, "—"), nz(in.Model, "—"),
				nz(in.Serial, "—"), nz(in.Firmware, "—"), nz(in.Calibration, "____"))
		}
		w("\n_Serial/firmware auto-captured from SCPI `*IDN?`; calibration dates to be completed from the vault._\n\n")
	}

	w("## 3. Results\n\n")
	w("Effective result per point (latest attempt). Superseded attempts are retained below.\n\n")
	w("| Point ID | From → To | Conductor | Expected mΩ | I (A) | V (mV) | Measured mΩ | Stable? | Att | Verdict | Observation |\n")
	w("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range s.Records {
		if r.Marker || r.Superseded {
			continue
		}
		w("| %s | %s | %s | %s | %s | %s | %s | %s | %d | %s | %s |\n",
			r.ID, r.Title,
			textVal(r, "conductor"),
			expectedCol(s, r.ID),
			numCol(r, "I"), numCol(r, "V"), numCol(r, "R"),
			boolCol(r, "stable"), r.Attempt,
			verdictLabel(r.Verdict), observation(r))
	}
	w("\nAcceptance: within expected **+50 %%** and stable = pass; **> 100 mΩ** investigate; open (source below 90 %% of set current), R ≤ 0, unstable or above expected +50 %% = fail; no expected value = not assessed (EN 60204-1 Cl. 18).\n\n")

	// Audit trail: superseded (re-measured) attempts are never discarded.
	var sup []model.Record
	for _, r := range s.Records {
		if r.Superseded && !r.Marker {
			sup = append(sup, r)
		}
	}
	if len(sup) > 0 {
		w("### 3.1 Superseded / re-measured attempts (retained)\n\n")
		w("| Point ID | Att | Captured (UTC) | I (A) | V (mV) | Measured mΩ | Verdict | Note |\n")
		w("|---|---|---|---|---|---|---|---|\n")
		for _, r := range sup {
			w("| %s | %d | %s | %s | %s | %s | %s | %s |\n",
				r.ID, r.Attempt, r.CapturedAt.Format("2006-01-02 15:04:05"),
				numCol(r, "I"), numCol(r, "V"), numCol(r, "R"),
				verdictLabel(r.Verdict), observation(r))
		}
		w("\n")
	}

	// Ambient markers.
	w("## 4. Ambient & overall evidence\n\n")
	for _, r := range s.Records {
		if !r.Marker {
			continue
		}
		w("- **%s** — %s (captured %s)\n", r.ID, r.Title, r.CapturedAt.Format(time.RFC3339))
		if v, ok := numValue(&r, "temp_c"); ok {
			w("  - ambient: %.1f °C", v)
			if h, ok := numValue(&r, "humidity"); ok {
				w(", %.0f %% RH", h)
			}
			w("\n")
		}
	}
	w("\n")

	w("## 5. Overall verdict\n\n")
	w("%s\n\n", overallVerdict(s))

	w("## 6. Evidence index\n\n")
	w("Photos correlate to records by **capture timestamp** (UTC):\n\n")
	w("| Record | Captured (UTC) | Title |\n|---|---|---|\n")
	for _, r := range s.Records {
		w("| %s | %s | %s |\n", r.ID, r.CapturedAt.UTC().Format("2006-01-02 15:04:05"), r.Title)
	}
	w("\n## 7. Feeds vault record\n\n")
	w("Feeds the product's electrical test evidence (Bonding Continuity — Results & Conclusion).\n")
	return b.String(), nil
}

// ---- helpers ----

func overallVerdict(s *model.Session) string {
	var fail, inv, open, judged int
	for _, r := range s.Records {
		if r.Marker || r.Superseded {
			continue
		}
		judged++
		switch r.Verdict {
		case model.VerdictFail:
			fail++
		case model.VerdictInvestigate:
			inv++
		case model.VerdictNotAssessed, model.VerdictPending, "":
			open++
		}
	}
	switch {
	case judged == 0:
		return "☐ No judged records."
	case fail > 0:
		msg := fmt.Sprintf("☑ **Fail** — %d of %d path(s) failed.", fail, judged)
		if open > 0 {
			msg += fmt.Sprintf(" %d further path(s) not assessed or not measured.", open)
		}
		return msg
	case open > 0:
		// "within acceptance" is only true of paths that were judged against one.
		return fmt.Sprintf("☐ **Incomplete** — %d of %d path(s) not assessed or not measured; no overall pass can be given.", open, judged)
	case inv > 0:
		return fmt.Sprintf("☐ Pass with %d path(s) to investigate.", inv)
	default:
		return fmt.Sprintf("☑ **Pass** — all %d path(s) within acceptance.", judged)
	}
}

func verdictLabel(v model.Verdict) string {
	switch v {
	case model.VerdictPass:
		return "Pass"
	case model.VerdictFail:
		return "**Fail**"
	case model.VerdictInvestigate:
		return "Investigate"
	case model.VerdictNotAssessed:
		return "Not assessed"
	case model.VerdictInfo:
		return "—"
	default:
		return "pending"
	}
}

func specExpected(spec model.RecordSpec, key string) (float64, bool) {
	for _, f := range spec.Fields {
		if f.Key == key && f.Expected != nil {
			return *f.Expected, true
		}
	}
	return 0, false
}

func expectedCol(s *model.Session, recID string) string {
	for _, spec := range s.Specs {
		if spec.ID == recID {
			if e, ok := specExpected(spec, "R"); ok {
				return fmt.Sprintf("%.1f", e)
			}
		}
	}
	return ""
}

func numValue(r *model.Record, key string) (float64, bool) {
	v, ok := r.Values[key]
	if !ok || v.Num == nil {
		return 0, false
	}
	return *v.Num, true
}

func boolValue(r *model.Record, key string) (bool, bool) {
	v, ok := r.Values[key]
	if !ok || v.Bool == nil {
		return false, false
	}
	return *v.Bool, true
}

func numCol(r model.Record, key string) string {
	if v, ok := numValue(&r, key); ok {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func boolCol(r model.Record, key string) string {
	if v, ok := boolValue(&r, key); ok {
		if v {
			return "yes"
		}
		return "no"
	}
	return ""
}

// observation joins the operator's note with the verdict's own reason, so a report
// row says WHY it failed or was not assessed, not only that it did.
func observation(r model.Record) string {
	var parts []string
	if n := textVal(r, "note"); n != "" {
		parts = append(parts, n)
	}
	if r.VerdictNote != "" {
		parts = append(parts, r.VerdictNote)
	}
	return strings.Join(parts, " · ")
}

func textVal(r model.Record, key string) string {
	if v, ok := r.Values[key]; ok {
		return v.Text
	}
	return ""
}

func paramFloat(params map[string]string, key string, def float64) float64 {
	if s, ok := params[key]; ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f
		}
	}
	return def
}

func nz(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

const defaultPoints = `# blank ID (line starts with |) -> auto-number B-01, B-02, …
# blank csa/length/joints/small -> repeats the row above (set a preset once)
# ID | title | csa_mm2 | length_m | joints | small?
| CBP → PELV 0 V bond | 2.5 | 0.3 | 1 | no
| CBP → DIN rail | 4 | 0.35 | 2 |
| CBP → panel (front) | | 0.4 | |
| CBP → panel (rear) | | 0.5 | |
| CBP → connector panel (Molex) | 2.5 | 0.45 | |
| CBP → serial/M12 connector shells | 1 | 0.5 | | yes
| CBP → shield/FE terminal | | 0.4 | 1 |`
