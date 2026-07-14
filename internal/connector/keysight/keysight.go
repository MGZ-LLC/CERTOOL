// Package keysight implements a connector for the Keysight E36312A triple-output
// programmable DC power supply, used as the floating constant-current source in
// the earth-continuity test (EN 60204-1 Cl. 18.2.2).
//
// Operation: select an output channel, set the voltage compliance low (e.g. 6 V)
// and the current to the test current; against a milliohm bonding path the supply
// runs in constant-current, and MEAS:CURR? reads the true delivered current.
//
// Channel note: on the E36312A only CH1 is the 6 V / 5 A output — keep the test
// current ≤ 5 A on CH1 (a typical bonding run uses ~3 A). CH2/CH3 are 25 V / 1 A.
package keysight

import (
	"fmt"
	"strconv"

	"github.com/MGZ-LLC/CERTOOL/internal/connector"
	"github.com/MGZ-LLC/CERTOOL/internal/connector/scpi"
	"github.com/MGZ-LLC/CERTOOL/internal/model"
)

type keysight struct {
	addr string
	ch   int
	t    scpi.Transport
	idn  string
}

func init() {
	connector.Register("keysight", func(addr string) connector.Connector {
		return &keysight{addr: addr, ch: 1}
	})
}

func (k *keysight) ID() string { return "keysight" }
func (k *keysight) Name() string {
	if k.idn != "" {
		return "Keysight E36312A [" + k.idn + "]"
	}
	return "Keysight E36312A (CC source)"
}

func (k *keysight) Capability() connector.Capability {
	return connector.Capability{
		CanApply:     true,
		CanRead:      true,
		ReadChannels: []string{"I", "V"},
		ApplyKeys:    []string{"current", "vlimit", "output", "channel"},
	}
}

func (k *keysight) Available() bool { return k.t != nil }

func (k *keysight) Connect() error {
	addr := k.addr
	if addr == "" {
		addr = scpi.DefaultUSB("Keysight") // /dev/usbtmc on Linux, pyvisa bridge on Windows
	}
	t, err := scpi.Open(addr)
	if err != nil {
		return err
	}
	k.t = t
	if idn, err := t.Query("*IDN?"); err == nil {
		k.idn = idn
	}
	return nil
}

func (k *keysight) Close() error {
	if k.t == nil {
		return nil
	}
	_ = k.t.Write(fmt.Sprintf("OUTP OFF,(@%d)", k.ch)) // safety
	err := k.t.Close()
	k.t = nil
	return err
}

func (k *keysight) Apply(settings map[string]string) error {
	if k.t == nil {
		return fmt.Errorf("keysight: not connected")
	}
	if v, ok := settings["channel"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 3 {
			k.ch = n
		}
	}
	if v, ok := settings["vlimit"]; ok {
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("keysight: bad vlimit %q", v)
		}
		if err := k.t.Write(fmt.Sprintf("VOLT %s,(@%d)", v, k.ch)); err != nil {
			return err
		}
	}
	if v, ok := settings["current"]; ok {
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("keysight: bad current %q", v)
		}
		if err := k.t.Write(fmt.Sprintf("CURR %s,(@%d)", v, k.ch)); err != nil {
			return err
		}
	}
	if v, ok := settings["output"]; ok {
		state := "OFF"
		if v == "on" || v == "1" || v == "true" {
			state = "ON"
		}
		if err := k.t.Write(fmt.Sprintf("OUTP %s,(@%d)", state, k.ch)); err != nil {
			return err
		}
	}
	return nil
}

// Instruments reports the Keysight identity captured at Connect().
func (k *keysight) Instruments() []model.Instrument {
	if k.idn == "" {
		return nil
	}
	return []model.Instrument{connector.ParseIDN("source (CC)", k.idn)}
}

func (k *keysight) Read(channels []string) ([]connector.Reading, error) {
	if k.t == nil {
		return nil, fmt.Errorf("keysight: not connected")
	}
	if len(channels) == 0 {
		channels = []string{"I"}
	}
	out := make([]connector.Reading, 0, len(channels))
	for _, ch := range channels {
		var query, unit string
		switch ch {
		case "I":
			query, unit = fmt.Sprintf("MEAS:CURR? (@%d)", k.ch), "A"
		case "V":
			query, unit = fmt.Sprintf("MEAS:VOLT? (@%d)", k.ch), "V"
		default:
			continue // this instrument doesn't provide the channel; let a composite handle it
		}
		s, err := k.t.Query(query)
		if err != nil {
			return nil, err
		}
		f, err := scpi.ParseFloat(s)
		if err != nil {
			return nil, fmt.Errorf("keysight: parse %q: %w", s, err)
		}
		out = append(out, connector.Reading{Channel: ch, Value: f, Unit: unit})
	}
	return out, nil
}
