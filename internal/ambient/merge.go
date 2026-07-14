package ambient

import (
	"fmt"
	"time"

	"github.com/MGZ-LLC/CERTOOL/internal/model"
)

// MergeResult summarises a merge.
type MergeResult struct {
	Matched int
	Total   int
	Tol     time.Duration
}

// Merge attaches ambient readings to a session's records by nearest timestamp,
// fills any blank ENV-marker ambient fields, registers the AZ logger as an
// instrument, and records a summary in Meta. tol is the largest time gap that
// still counts as a match.
func Merge(s *model.Session, log *Log, tol time.Duration) MergeResult {
	res := MergeResult{Tol: tol}
	for i := range s.Records {
		r := &s.Records[i]
		res.Total++
		sample, ok := log.NearestWithin(r.CapturedAt, tol)
		if !ok {
			continue
		}
		res.Matched++
		setNum(r, "ambient_temp_c", sample.TempC)
		setNum(r, "ambient_rh", sample.RH)
		setNum(r, "ambient_pressure", sample.Pressure)
		if r.Marker { // let a real logger fill the baseline the operator may have skipped
			fillIfEmpty(r, "temp_c", sample.TempC)
			fillIfEmpty(r, "humidity", sample.RH)
			fillIfEmpty(r, "pressure", sample.Pressure)
		}
	}

	addInstrument(s, model.Instrument{
		Role: "ambient logger", Manufacturer: "AZ Instrument", Model: "88163", Serial: log.Serial,
	})
	if s.Meta == nil {
		s.Meta = map[string]string{}
	}
	tzH := log.TZOffsetMin / 60
	s.Meta["ambient_source"] = fmt.Sprintf("AZ 88163 SN %s — %d samples, %s, logger tz UTC%+d",
		log.Serial, len(log.Samples), nz(log.SamplingText, "?"), tzH)
	return res
}

func setNum(r *model.Record, key string, v float64) {
	if r.Values == nil {
		r.Values = map[string]model.Value{}
	}
	f := v
	r.Values[key] = model.Value{Key: key, Num: &f, Source: model.SourceMeta}
}

func fillIfEmpty(r *model.Record, key string, v float64) {
	if r.Values == nil {
		r.Values = map[string]model.Value{}
	}
	if ex, ok := r.Values[key]; ok && ex.Num != nil {
		return // operator already entered a value; don't overwrite
	}
	f := v
	r.Values[key] = model.Value{Key: key, Num: &f, Source: model.SourceMeta}
}

func addInstrument(s *model.Session, in model.Instrument) {
	for _, e := range s.Instruments {
		if e.Serial != "" && e.Serial == in.Serial {
			return
		}
	}
	s.Instruments = append(s.Instruments, in)
}

func nz(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
