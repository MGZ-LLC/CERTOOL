// Package connector defines the device-connector interface: how the framework
// forces settings onto an instrument and reads values back. Every campaign needs
// at least the built-in "manual" connector, which is the smart manual-entry
// fallback — it captures nothing automatically but lets the UI present expected
// values for the operator to confirm/override. Real connectors (e.g. Korad)
// implement the same interface and can be swapped in transparently.
package connector

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MGZ-LLC/CERTOOL/internal/model"
)

// Capability advertises what a connector can do, so the UI can show or hide the
// "read"/"apply" affordances per instrument.
type Capability struct {
	CanApply     bool
	CanRead      bool
	ReadChannels []string // e.g. ["I", "V"]
	ApplyKeys    []string // e.g. ["current", "vlimit", "output"]
}

// Reading is one value read back from an instrument.
type Reading struct {
	Channel string  `json:"channel"`
	Value   float64 `json:"value"`
	Unit    string  `json:"unit"`
}

// Connector is the contract for a device connector. Implementations must be safe
// to construct without hardware present; Connect() is where a missing device is
// reported, and Available() reflects whether a live link exists.
type Connector interface {
	ID() string
	Name() string
	Capability() Capability
	Connect() error
	Close() error
	Available() bool
	Apply(settings map[string]string) error
	Read(channels []string) ([]Reading, error)
}

// Identifiable is optionally implemented by connectors that can report the
// instruments they front (from SCPI *IDN?), so the framework records them as
// test evidence without manual typing.
type Identifiable interface {
	Instruments() []model.Instrument
}

// ParseIDN turns a standard "*IDN?" reply (Manufacturer,Model,Serial,Firmware)
// into an Instrument, tagged with the given role. Missing fields are left blank.
func ParseIDN(role, idn string) model.Instrument {
	inst := model.Instrument{Role: role, IDN: strings.TrimSpace(idn)}
	parts := strings.SplitN(idn, ",", 4)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) > 0 {
		inst.Manufacturer = parts[0]
	}
	if len(parts) > 1 {
		inst.Model = parts[1]
	}
	if len(parts) > 2 {
		inst.Serial = parts[2]
	}
	if len(parts) > 3 {
		inst.Firmware = parts[3]
	}
	return inst
}

// Factory builds a connector instance from an opaque address string (e.g. a
// serial port). The address may be empty for connectors that need none.
type Factory func(addr string) Connector

var registry = map[string]Factory{}

// Register makes a connector factory discoverable by ID. Called from init().
func Register(id string, f Factory) {
	if _, dup := registry[id]; dup {
		panic(fmt.Sprintf("connector: duplicate id %q", id))
	}
	registry[id] = f
}

// New constructs a connector by ID.
func New(id, addr string) (Connector, error) {
	f, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("connector: unknown id %q", id)
	}
	return f(addr), nil
}

// IDs lists registered connector IDs, sorted, for the UI menu.
func IDs() []string {
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
