package connector

// manual is the default fallback connector: no instrument link, everything is
// entered by hand. It is always "available" so a campaign can run on any machine
// with no hardware attached. The UI pairs it with the expected-value prefill so
// manual entry is still guided ("smart manual entry").
type manual struct{}

func init() { Register("manual", func(addr string) Connector { return manual{} }) }

func (manual) ID() string   { return "manual" }
func (manual) Name() string { return "Manual entry (smart fallback)" }

func (manual) Capability() Capability {
	return Capability{CanApply: false, CanRead: false}
}

func (manual) Connect() error  { return nil }
func (manual) Close() error    { return nil }
func (manual) Available() bool { return true }

// Apply is a no-op: with the manual connector the operator sets the instrument
// by hand, guided by the spec's DeviceSettings shown in the UI.
func (manual) Apply(map[string]string) error { return nil }

// Read returns nothing; the UI falls back to manual field entry.
func (manual) Read([]string) ([]Reading, error) { return nil, nil }
