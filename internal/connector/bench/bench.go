// Package bench provides a composite connector that combines a current SOURCE
// and a sense METER into one logical instrument for four-wire measurements such
// as earth continuity: the Keysight E36312A sources the test current (channel
// "I") and the Rigol DM858E reads the millivolt drop (channel "V"). Selecting
// this single connector wires up the whole four-wire rig.
//
// Address forms:
//
//	""                                   auto-discover both by USBTMC *IDN*
//	"source=usbtmc:@Keysight;meter=usbtmc:@Rigol"
//	"source=/dev/usbtmc1;meter=/dev/usbtmc2"
//	"source=lan:192.168.0.5;meter=lan:192.168.0.6"
package bench

import (
	"fmt"
	"strings"

	"github.com/MGZ-LLC/CERTOOL/internal/connector"
	"github.com/MGZ-LLC/CERTOOL/internal/model"
	// Blank-imported so their factories ("keysight", "rigol") are registered
	// before this composite's factory constructs them via connector.New.
	_ "github.com/MGZ-LLC/CERTOOL/internal/connector/keysight"
	_ "github.com/MGZ-LLC/CERTOOL/internal/connector/rigol"
)

type bench struct {
	sourceAddr, meterAddr string
	source, meter         connector.Connector
}

func init() {
	connector.Register("bench-4wire", func(addr string) connector.Connector {
		b := &bench{}
		for _, part := range strings.Split(addr, ";") {
			part = strings.TrimSpace(part)
			if k, v, ok := strings.Cut(part, "="); ok {
				switch strings.TrimSpace(k) {
				case "source":
					b.sourceAddr = strings.TrimSpace(v)
				case "meter":
					b.meterAddr = strings.TrimSpace(v)
				}
			}
		}
		// Build sub-connectors via their registered factories.
		b.source, _ = connector.New("keysight", b.sourceAddr)
		b.meter, _ = connector.New("rigol", b.meterAddr)
		return b
	})
}

func (b *bench) ID() string   { return "bench-4wire" }
func (b *bench) Name() string {
	return fmt.Sprintf("4-wire bench [%s + %s]", short(b.source), short(b.meter))
}

func (b *bench) Capability() connector.Capability {
	return connector.Capability{
		CanApply:     true,
		CanRead:      true,
		ReadChannels: []string{"I", "V"},
		ApplyKeys:    []string{"current", "vlimit", "output", "channel"},
	}
}

func (b *bench) Available() bool {
	return b.source != nil && b.meter != nil && b.source.Available() && b.meter.Available()
}

func (b *bench) Connect() error {
	if b.source == nil || b.meter == nil {
		return fmt.Errorf("bench: sub-connectors not built")
	}
	if err := b.source.Connect(); err != nil {
		return fmt.Errorf("bench source (Keysight): %w", err)
	}
	if err := b.meter.Connect(); err != nil {
		return fmt.Errorf("bench meter (Rigol): %w", err)
	}
	return nil
}

func (b *bench) Close() error {
	var errs []string
	if b.source != nil {
		if err := b.source.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if b.meter != nil {
		if err := b.meter.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bench close: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Apply routes source settings (current/vlimit/output/channel) to the PSU.
func (b *bench) Apply(settings map[string]string) error {
	if b.source == nil {
		return fmt.Errorf("bench: no source")
	}
	return b.source.Apply(settings)
}

// Read routes channel "I" to the source and "V" to the meter.
func (b *bench) Read(channels []string) ([]connector.Reading, error) {
	if len(channels) == 0 {
		channels = []string{"I", "V"}
	}
	var out []connector.Reading
	for _, ch := range channels {
		switch strings.ToUpper(ch) {
		case "I":
			r, err := b.source.Read([]string{"I"})
			if err != nil {
				return nil, err
			}
			out = append(out, r...)
		case "V":
			r, err := b.meter.Read([]string{"V"})
			if err != nil {
				return nil, err
			}
			out = append(out, r...)
		}
	}
	return out, nil
}

// Instruments aggregates the identities of the source and meter sub-connectors.
func (b *bench) Instruments() []model.Instrument {
	var out []model.Instrument
	for _, c := range []connector.Connector{b.source, b.meter} {
		if id, ok := c.(connector.Identifiable); ok {
			out = append(out, id.Instruments()...)
		}
	}
	return out
}

func short(c connector.Connector) string {
	if c == nil {
		return "?"
	}
	return c.ID()
}
