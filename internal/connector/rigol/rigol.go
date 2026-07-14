// Package rigol implements a connector for the Rigol DM858E bench digital
// multimeter, used as the four-wire sense voltmeter in the earth-continuity test.
//
// The bonding drop is a few millivolts, and the test computes R = V/I in mΩ with
// I in amps, so this connector returns the "V" channel in MILLIVOLTS (the SCPI
// reply is in volts and is scaled ×1000) to line up with the test app's units.
package rigol

import (
	"fmt"
	"strings"

	"github.com/MGZ-LLC/CERTOOL/internal/connector"
	"github.com/MGZ-LLC/CERTOOL/internal/connector/scpi"
	"github.com/MGZ-LLC/CERTOOL/internal/model"
)

type rigol struct {
	addr string
	t    scpi.Transport
	idn  string
}

func init() {
	connector.Register("rigol", func(addr string) connector.Connector {
		return &rigol{addr: addr}
	})
}

func (r *rigol) ID() string { return "rigol" }
func (r *rigol) Name() string {
	if r.idn != "" {
		return "Rigol DM858E [" + r.idn + "]"
	}
	return "Rigol DM858E (DMM, mV DC)"
}

func (r *rigol) Capability() connector.Capability {
	return connector.Capability{
		CanApply:     true, // can pre-select DC-V function / range
		CanRead:      true,
		ReadChannels: []string{"V"},
		ApplyKeys:    []string{"function", "range"},
	}
}

func (r *rigol) Available() bool { return r.t != nil }

func (r *rigol) Connect() error {
	addr := r.addr
	if addr == "" {
		addr = scpi.DefaultUSB("Rigol")
	}
	t, err := scpi.Open(addr)
	if err != nil {
		return err
	}
	r.t = t
	if idn, err := t.Query("*IDN?"); err == nil {
		r.idn = idn
	}
	// Pre-select DC voltage so the first read is fast.
	_ = t.Write(":FUNCtion VOLTage:DC")
	return nil
}

func (r *rigol) Close() error {
	if r.t == nil {
		return nil
	}
	err := r.t.Close()
	r.t = nil
	return err
}

func (r *rigol) Apply(settings map[string]string) error {
	if r.t == nil {
		return fmt.Errorf("rigol: not connected")
	}
	if _, ok := settings["function"]; ok {
		if err := r.t.Write(":FUNCtion VOLTage:DC"); err != nil {
			return err
		}
	}
	if v, ok := settings["range"]; ok {
		if err := r.t.Write(":SENSe:VOLTage:DC:RANGe " + v); err != nil {
			return err
		}
	}
	return nil
}

// Instruments reports the Rigol identity captured at Connect().
func (r *rigol) Instruments() []model.Instrument {
	if r.idn == "" {
		return nil
	}
	return []model.Instrument{connector.ParseIDN("meter (mV)", r.idn)}
}

func (r *rigol) Read(channels []string) ([]connector.Reading, error) {
	if r.t == nil {
		return nil, fmt.Errorf("rigol: not connected")
	}
	if len(channels) == 0 {
		channels = []string{"V"}
	}
	out := make([]connector.Reading, 0, len(channels))
	for _, ch := range channels {
		if strings.ToUpper(ch) != "V" {
			continue // DMM only provides the voltage channel here
		}
		s, err := r.t.Query(":MEASure:VOLTage:DC?")
		if err != nil {
			return nil, err
		}
		volts, err := scpi.ParseFloat(s)
		if err != nil {
			return nil, fmt.Errorf("rigol: parse %q: %w", s, err)
		}
		out = append(out, connector.Reading{Channel: "V", Value: volts * 1000.0, Unit: "mV"})
	}
	return out, nil
}
