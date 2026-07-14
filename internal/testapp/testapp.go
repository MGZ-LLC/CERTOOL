// Package testapp defines the interface every specialised test application
// implements, plus a registry so the framework can discover them by ID.
//
// A test app owns the domain knowledge for one kind of measurement campaign:
// what setup inputs it needs, how to turn those into an ordered list of record
// specs (with expected values prefilled for smart manual entry), how to compute
// derived fields and a verdict once a record is captured, and how to render the
// Obsidian report. The framework knows nothing about earth continuity, EMC, etc.
package testapp

import (
	"fmt"
	"sort"

	"github.com/MGZ-LLC/CERTOOL/internal/model"
)

// Info is the human-facing identity of a test app.
type Info struct {
	ID          string
	Name        string
	Standard    string
	Description string
}

// TestApp is the contract for a specialised test.
type TestApp interface {
	Info() Info

	// SetupFields returns the app-specific parameters collected on the setup
	// screen (in addition to the common session metadata). For earth continuity
	// these describe the bonding points to measure.
	SetupFields() []model.FieldSpec

	// Prepare turns the collected setup parameters into the ordered run sheet of
	// record specs, prefilling expected values wherever they can be computed.
	Prepare(s *model.Session, params map[string]string) ([]model.RecordSpec, error)

	// PointFields returns the inputs needed to define ONE additional record spec
	// mid-run (incremental points). Return nil if the app does not support adding
	// points during a run.
	PointFields() []model.FieldSpec

	// NewPoint builds a single record spec from PointFields values, so the
	// operator can add a measurement point during the test. Return an error if
	// unsupported or the params are invalid.
	NewPoint(s *model.Session, params map[string]string) (model.RecordSpec, error)

	// Evaluate computes derived fields (e.g. R = V/I) and sets the verdict on a
	// record that has just been captured, according to the spec it came from.
	Evaluate(spec model.RecordSpec, rec *model.Record) error

	// Report renders the Obsidian-flavoured markdown report body for a session.
	Report(s *model.Session) (string, error)
}

// Finisher is optionally implemented by a test app to surface manual-operation
// instructions on the finish page — e.g. stopping a data logger and exporting
// its file. Used for instruments that cannot be automated live.
type Finisher interface {
	FinishInstructions() []string
}

var registry = map[string]TestApp{}

// Register makes a test app discoverable by its ID. Called from app packages'
// init(); duplicate IDs panic at startup.
func Register(a TestApp) {
	id := a.Info().ID
	if _, dup := registry[id]; dup {
		panic(fmt.Sprintf("testapp: duplicate id %q", id))
	}
	registry[id] = a
}

// Get returns the app with the given ID, or an error if unknown.
func Get(id string) (TestApp, error) {
	a, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("testapp: unknown id %q", id)
	}
	return a, nil
}

// List returns all registered apps, sorted by ID, for the UI menu.
func List() []Info {
	out := make([]Info, 0, len(registry))
	for _, a := range registry {
		out = append(out, a.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
