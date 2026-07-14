// Package model defines the platform-agnostic data types shared by the whole
// certool framework: sessions, prepared record specs, captured records
// and their fields. Nothing here is specific to any single test or instrument —
// test apps (internal/testapp) and device connectors (internal/connector) are
// built on top of these types.
package model

import "time"

// FieldType classifies how a field is captured and rendered in the UI.
type FieldType string

const (
	FieldNumber FieldType = "number"
	FieldText   FieldType = "text"
	FieldBool   FieldType = "bool"
	FieldChoice FieldType = "choice"
)

// FieldSource says where a field's value is meant to come from. It drives the
// UI: device fields get a "read" button, computed fields are read-only, manual
// fields are typed in (with the expected value prefilled where known).
type FieldSource string

const (
	SourceManual   FieldSource = "manual"   // operator types it
	SourceDevice   FieldSource = "device"   // read from an instrument connector
	SourceComputed FieldSource = "computed" // derived by the test app (e.g. R = V/I)
	SourceMeta     FieldSource = "meta"     // session metadata / auto-filled
)

// FieldSpec describes one field of a record — effectively one column of the
// results table. The Expected/ExpectedText fields carry the "smart manual entry"
// prefill: the value the operator should see before measuring.
type FieldSpec struct {
	Key           string      `json:"key"`
	Label         string      `json:"label"`
	Type          FieldType   `json:"type"`
	Source        FieldSource `json:"source"`
	Unit          string      `json:"unit,omitempty"`
	Choices       []string    `json:"choices,omitempty"`
	Expected      *float64    `json:"expected,omitempty"`      // prefilled numeric expectation
	ExpectedText  string      `json:"expected_text,omitempty"` // prefilled text expectation
	Required      bool        `json:"required"`
	DeviceChannel string      `json:"device_channel,omitempty"` // maps to a connector read channel
	ReadOnly      bool        `json:"read_only,omitempty"`
	Help          string      `json:"help,omitempty"`
}

// RecordSpec is a *prepared* measurement set — one step of the run sheet,
// produced by a test app's Prepare(). DeviceSettings are forced onto the active
// connector before capture (e.g. Korad constant-current value and voltage limit).
type RecordSpec struct {
	ID             string            `json:"id"`    // e.g. "B-01"
	Title          string            `json:"title"` // e.g. "CBP → DIN rail"
	Note           string            `json:"note,omitempty"`
	Fields         []FieldSpec       `json:"fields"`
	DeviceSettings map[string]string `json:"device_settings,omitempty"`
	Marker         bool              `json:"marker,omitempty"` // true for ambient/photo prompts, not judged
}

// Value holds a captured field value. Exactly one of Num/Text/Bool is used,
// per the field's type.
type Value struct {
	Key    string      `json:"key"`
	Num    *float64    `json:"num,omitempty"`
	Text   string      `json:"text,omitempty"`
	Bool   *bool       `json:"bool,omitempty"`
	Source FieldSource `json:"source"`
}

// Verdict is the acceptance outcome of a record.
type Verdict string

const (
	VerdictPending     Verdict = "pending"
	VerdictPass        Verdict = "pass"
	VerdictInvestigate Verdict = "investigate"
	VerdictFail        Verdict = "fail"
	VerdictInfo        Verdict = "info" // non-judged records (ambient, overall photos)
)

// Attachment records an out-of-band evidence item correlated by timestamp —
// e.g. a photo taken with a separate camera. The Time is the correlation key:
// files with a matching capture time belong to this record.
type Attachment struct {
	Kind string    `json:"kind"` // photo | screenshot | log
	Note string    `json:"note,omitempty"`
	File string    `json:"file,omitempty"`
	Time time.Time `json:"time"`
}

// Record is a committed measurement set. CapturedAt (UTC) is the timestamp that
// lets externally-taken photos be related back to this record.
type Record struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Values      map[string]Value `json:"values"`
	Verdict     Verdict          `json:"verdict"`
	VerdictNote string           `json:"verdict_note,omitempty"`
	CapturedAt  time.Time        `json:"captured_at"`
	Attachments []Attachment     `json:"attachments,omitempty"`
	Marker      bool             `json:"marker,omitempty"`
	// Attempt is the 1-based measurement attempt for this point ID; a re-measure
	// appends a new record with Attempt+1 and marks the previous ones Superseded.
	// Records are never overwritten — the full history is retained as evidence.
	Attempt    int  `json:"attempt"`
	Superseded bool `json:"superseded,omitempty"`
}

// Instrument records the identity of a measurement instrument used in a session,
// captured automatically from its SCPI *IDN? where possible. Calibration is left
// for the operator/vault to complete — it is required CE test evidence.
type Instrument struct {
	Role         string `json:"role,omitempty"` // e.g. "source", "meter"
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Serial       string `json:"serial,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
	IDN          string `json:"idn,omitempty"`
	Calibration  string `json:"calibration,omitempty"`
}

// Session is one run of a test app against an equipment-under-test (EUT).
type Session struct {
	ID        string            `json:"id"`
	TestApp   string            `json:"test_app"`
	Title     string            `json:"title"`
	Product   string            `json:"product"`
	Serial    string            `json:"serial"`
	ConfigRef string            `json:"config_ref"`
	WiringRef string            `json:"wiring_ref"`
	Operator  string            `json:"operator"`
	Witness   string            `json:"witness"`
	Location  string            `json:"location"`
	Standard  string            `json:"standard"`
	Connector string            `json:"connector"`
	StartedAt time.Time         `json:"started_at"`
	UpdatedAt time.Time         `json:"updated_at"` // touched on every persist; used to pick the latest resumable session
	Closed    bool              `json:"closed,omitempty"` // set by Finish; a closed session is not offered for resume
	ConnectorAddr string        `json:"connector_addr,omitempty"` // so the connector can be rebuilt on resume
	OutputDir   string          `json:"output_dir"`
	Instruments []Instrument    `json:"instruments,omitempty"`
	Specs     []RecordSpec      `json:"specs"`
	Records   []Record          `json:"records"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// Num is a small helper for building *float64 literals.
func Num(f float64) *float64 { return &f }
