package earthcontinuity

import (
	"strings"
	"testing"

	"github.com/MGZ-LLC/CERTOOL/internal/model"
)

// reading builds a committed record the way the engine does: device I and V,
// manual stable flag.
func reading(i, v float64, stable bool) *model.Record {
	return &model.Record{Values: map[string]model.Value{
		"I":      {Key: "I", Num: model.Num(i), Source: model.SourceDevice},
		"V":      {Key: "V", Num: model.Num(v), Source: model.SourceDevice},
		"stable": {Key: "stable", Bool: &stable, Source: model.SourceManual},
	}}
}

// withExp: 1.5 mm² × 0.23 m, 3 joints → expected ≈ 17.6 mΩ, pass ≤ 26.4 mΩ.
var withExp = buildPointSpec(point{id: "B-01", title: "panel", csa: 1.5, length: 0.23, joints: 3}, 3, 1, 6, 1)

// noExp: a point added without conductor geometry — no computed expectation.
var noExp = buildPointSpec(point{id: "B-02", title: "added mid-run"}, 3, 1, 6, 1)

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name string
		spec model.RecordSpec
		rec  *model.Record
		want model.Verdict
	}{
		// The regression: source at its 6 V limit, read-back current a hair
		// below zero → R hugely negative, which used to satisfy R ≤ expected×1.5.
		{"open circuit, negative I, with expectation", withExp, reading(-0.000018, 5999.9, true), model.VerdictFail},
		{"open circuit, negative I, no expectation", noExp, reading(-0.000027, 5999.98, true), model.VerdictFail},
		// Tiny positive I → megaohms, which used to stop at "investigate".
		{"open circuit, tiny positive I", withExp, reading(0.000574, 6000.8, true), model.VerdictFail},
		{"source out of regulation, half the set current", withExp, reading(1.5, 5999, true), model.VerdictFail},
		{"exactly zero current", withExp, reading(0, 6000, true), model.VerdictFail},
		{"good bond within expected+50 %", withExp, reading(2.999314, 56.2, true), model.VerdictPass},
		{"above expected+50 %", withExp, reading(2.999314, 207.5, true), model.VerdictFail},
		{"unstable while flexing", withExp, reading(2.999478, 45.0, false), model.VerdictFail},
		{"unstable AND above 100 mΩ fails, not investigate", noExp, reading(2.999478, 657.0, false), model.VerdictFail},
		{"reversed sense leads, R < 0 at full current", withExp, reading(2.999478, -40.0, true), model.VerdictFail},
		{"above 100 mΩ with no expectation → investigate", noExp, reading(2.999478, 657.0, true), model.VerdictInvestigate},
		{"good reading, no expectation → not assessed, never pass", noExp, reading(2.999478, 45.0, true), model.VerdictNotAssessed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := (app{}).Evaluate(c.spec, c.rec); err != nil {
				t.Fatal(err)
			}
			if c.rec.Verdict != c.want {
				t.Fatalf("verdict %q, want %q (note: %s)", c.rec.Verdict, c.want, c.rec.VerdictNote)
			}
			if c.want != model.VerdictPass && c.rec.VerdictNote == "" {
				t.Errorf("verdict %q carries no reason", c.rec.Verdict)
			}
		})
	}
}

func TestReevaluationClearsStaleNote(t *testing.T) {
	rec := reading(-0.000018, 5999.9, true)
	_ = (app{}).Evaluate(withExp, rec)
	rec.Values["I"] = model.Value{Key: "I", Num: model.Num(2.999314), Source: model.SourceDevice}
	rec.Values["V"] = model.Value{Key: "V", Num: model.Num(40.0), Source: model.SourceDevice}
	_ = (app{}).Evaluate(withExp, rec)
	if rec.Verdict != model.VerdictPass || rec.VerdictNote != "" {
		t.Fatalf("got %q with note %q after a good re-measurement", rec.Verdict, rec.VerdictNote)
	}
}

func TestOverallVerdict(t *testing.T) {
	rec := func(v model.Verdict) model.Record { return model.Record{Verdict: v} }
	cases := []struct {
		name    string
		records []model.Record
		want    string
		never   string
	}{
		{"all pass", []model.Record{rec(model.VerdictPass), rec(model.VerdictPass)}, "**Pass** — all 2", ""},
		{"not assessed blocks an overall pass", []model.Record{rec(model.VerdictPass), rec(model.VerdictNotAssessed)}, "**Incomplete**", "within acceptance"},
		{"unmeasured blocks an overall pass", []model.Record{rec(model.VerdictPass), rec(model.VerdictPending)}, "**Incomplete**", "within acceptance"},
		{"fail reports the unassessed too", []model.Record{rec(model.VerdictFail), rec(model.VerdictNotAssessed)}, "1 further path(s) not assessed", ""},
		{"investigate", []model.Record{rec(model.VerdictPass), rec(model.VerdictInvestigate)}, "1 path(s) to investigate", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := overallVerdict(&model.Session{Records: c.records})
			if !strings.Contains(got, c.want) {
				t.Fatalf("got %q, want it to contain %q", got, c.want)
			}
			if c.never != "" && strings.Contains(got, c.never) {
				t.Fatalf("got %q, must not contain %q", got, c.never)
			}
		})
	}
}

func TestReportShowsWhy(t *testing.T) {
	r := reading(-0.000018, 5999.9, true)
	r.ID, r.Title, r.Attempt = "B-01", "panel", 1
	_ = (app{}).Evaluate(withExp, r)
	s := &model.Session{Product: "Example", Specs: []model.RecordSpec{withExp}, Records: []model.Record{*r}}
	out, err := (app{}).Report(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**Fail**", "Open circuit or gross resistance", "**Recorded with:** certool/"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}
