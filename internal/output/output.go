// Package output writes a session's results to disk in the documented layout:
//
//	<output>/<session-id>/
//	  session.json                 — machine-readable full record
//	  Report.md                    — Obsidian report (from the test app)
//	  results.xlsx                 — calculated tabular data (one row per record)
//	  evidence/photos/             — drop externally-taken photos here
//	  evidence/instrument-logs/
//	  evidence/screenshots/
//	  README.md                    — how photos correlate by timestamp
//
// Everything is designed to live inside an Obsidian vault: Report.md uses
// wikilinks and the folder is self-describing.
package output

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/MGZ-LLC/CERTOOL/internal/model"
	"github.com/MGZ-LLC/CERTOOL/internal/testapp"
	"github.com/xuri/excelize/v2"
)

// EvidenceDirs are created empty for the operator to populate.
var EvidenceDirs = []string{
	filepath.Join("evidence", "photos"),
	filepath.Join("evidence", "instrument-logs"),
	filepath.Join("evidence", "screenshots"),
}

// Load reads a session back from its directory's session.json.
func Load(sessionDir string) (*model.Session, error) {
	b, err := os.ReadFile(filepath.Join(sessionDir, "session.json"))
	if err != nil {
		return nil, err
	}
	var s model.Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Resumables walks the output tree and returns every un-closed session (left
// mid-run), most-recently-updated first. Each has OutputDir set so it can be
// resumed by folder.
func Resumables(root string) []*model.Session {
	var out []*model.Session
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "session.json" {
			return nil
		}
		s, e := Load(filepath.Dir(path))
		if e != nil || s.Closed {
			return nil
		}
		if s.OutputDir == "" {
			s.OutputDir = filepath.Dir(path)
		}
		out = append(out, s)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// LatestResumable returns the most recently updated un-closed session.
func LatestResumable(root string) (*model.Session, bool) {
	r := Resumables(root)
	if len(r) == 0 {
		return nil, false
	}
	return r[0], true
}

// SessionDir is the organised output path for a session:
//
//	<root>/<EUT>/<test-app>/<YYYY-MM-DD_HHMMSS>
//
// so material is grouped by equipment-under-test, then test, then dated run.
func SessionDir(root string, s *model.Session) string {
	eut := safeName(s.Product)
	if eut == "" {
		eut = "EUT"
	}
	app := safeName(s.TestApp)
	if app == "" {
		app = "test"
	}
	stamp := s.StartedAt.Format("2006-01-02_150405")
	return filepath.Join(root, eut, app, stamp)
}

// safeName makes a filesystem-safe path segment.
func safeName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ', r == '-', r == '_', r == '.':
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}

// Prepare creates the session folder + evidence subfolders and returns the
// session directory. Safe to call repeatedly.
func Prepare(root string, s *model.Session) (string, error) {
	dir := SessionDir(root, s)
	for _, d := range EvidenceDirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// Write emits session.json, Report.md, results.xlsx and README.md into the
// session directory. app renders the report body.
func Write(dir string, s *model.Session, app testapp.TestApp) error {
	if err := writeJSON(filepath.Join(dir, "session.json"), s); err != nil {
		return err
	}
	body, err := app.Report(s)
	if err != nil {
		return fmt.Errorf("render report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Report.md"), []byte(body), 0o644); err != nil {
		return err
	}
	if err := writeXLSX(filepath.Join(dir, "results.xlsx"), s); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme(s)), 0o644)
}

func writeJSON(path string, s *model.Session) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// writeXLSX builds one sheet: a column per field encountered across specs, one
// row per captured record, plus timestamp and verdict columns. This is the
// "spreadsheet for calculated tabular data".
func writeXLSX(path string, s *model.Session) error {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Results"
	f.SetSheetName("Sheet1", sheet)

	// Stable, ordered column set derived from the specs.
	cols := []string{"Record", "Attempt", "Superseded", "Title", "Captured (UTC)"}
	seen := map[string]bool{}
	for _, spec := range s.Specs {
		for _, fld := range spec.Fields {
			if !seen[fld.Key] {
				seen[fld.Key] = true
				label := fld.Label
				if fld.Unit != "" {
					label += " (" + fld.Unit + ")"
				}
				cols = append(cols, label)
			}
		}
	}
	cols = append(cols, "Verdict", "Verdict note")

	// Header.
	for i, c := range cols {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, c)
	}
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if hdrEnd, err := excelize.CoordinatesToCellName(len(cols), 1); err == nil {
		f.SetCellStyle(sheet, "A1", hdrEnd, style)
	}

	// Column key order matching header (after the 3 fixed cols).
	var keyOrder []string
	seen = map[string]bool{}
	for _, spec := range s.Specs {
		for _, fld := range spec.Fields {
			if !seen[fld.Key] {
				seen[fld.Key] = true
				keyOrder = append(keyOrder, fld.Key)
			}
		}
	}

	const base = 5 // fixed columns before the per-field values
	row := 2
	for _, r := range s.Records {
		setCell(f, sheet, 1, row, r.ID)
		setCell(f, sheet, 2, row, r.Attempt)
		supersededYN := ""
		if r.Superseded {
			supersededYN = "yes"
		}
		setCell(f, sheet, 3, row, supersededYN)
		setCell(f, sheet, 4, row, r.Title)
		setCell(f, sheet, 5, row, r.CapturedAt.UTC().Format("2006-01-02 15:04:05"))
		for j, key := range keyOrder {
			col := base + 1 + j
			if v, ok := r.Values[key]; ok {
				setCell(f, sheet, col, row, valueCell(v))
			}
		}
		setCell(f, sheet, base+1+len(keyOrder), row, string(r.Verdict))
		setCell(f, sheet, base+2+len(keyOrder), row, r.VerdictNote)
		row++
	}

	return f.SaveAs(path)
}

func setCell(f *excelize.File, sheet string, col, row int, v any) {
	cell, err := excelize.CoordinatesToCellName(col, row)
	if err != nil {
		return
	}
	f.SetCellValue(sheet, cell, v)
}

func valueCell(v model.Value) any {
	switch {
	case v.Num != nil:
		return *v.Num
	case v.Bool != nil:
		if *v.Bool {
			return "yes"
		}
		return "no"
	default:
		return v.Text
	}
}

func readme(s *model.Session) string {
	return "# " + s.Title + "\n\n" +
		"Session `" + s.ID + "` — generated by certool.\n\n" +
		"## Photo correlation\n\n" +
		"Each record in `Report.md` / `results.xlsx` carries a **UTC capture timestamp**. " +
		"Photos taken at the bench (with any camera) are matched to a record by that timestamp — " +
		"drop them in `evidence/photos/` and name them with the record ID where known " +
		"(e.g. `" + s.Product + "_" + firstNonEmpty(s.TestApp, "TP") + "_B-02_...`).\n\n" +
		"- `session.json` — full machine-readable record\n" +
		"- `Report.md` — Obsidian report (open in the vault)\n" +
		"- `results.xlsx` — calculated tabular data\n" +
		"- `evidence/` — photos, instrument logs, screenshots\n"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// FormatFloat is exported for the web layer to render values consistently.
func FormatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
