// Package engine holds the live run state and drives the capture loop that the
// web UI advances one record at a time. The run sheet is a GROWABLE catalogue of
// point specs with a pending queue: points can be prepared upfront, added
// incrementally during the test, or re-measured. Records are APPEND-ONLY — a
// re-measure appends a new attempt and marks earlier ones superseded, so nothing
// is ever overwritten and the full history survives as test evidence.
package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/MGZ-LLC/CERTOOL/internal/ambient"
	"github.com/MGZ-LLC/CERTOOL/internal/connector"
	"github.com/MGZ-LLC/CERTOOL/internal/model"
	"github.com/MGZ-LLC/CERTOOL/internal/output"
	"github.com/MGZ-LLC/CERTOOL/internal/testapp"
)

// Engine is a single active session, safe for concurrent HTTP handlers.
type Engine struct {
	mu    sync.Mutex
	root  string
	app   testapp.TestApp
	conn  connector.Connector
	sess  *model.Session
	dir   string
	queue []int // indices into sess.Specs awaiting capture (front = current)
	now   func() time.Time
}

// New creates an idle engine writing sessions under root.
func New(root string) *Engine {
	return &Engine{root: root, now: time.Now}
}

// Start prepares a new session and enqueues every prepared spec.
func (e *Engine) Start(app testapp.TestApp, conn connector.Connector, sess *model.Session, setup map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	sess.TestApp = app.Info().ID
	sess.Standard = app.Info().Standard
	sess.Connector = conn.ID()
	sess.StartedAt = e.now()
	if id, ok := conn.(connector.Identifiable); ok {
		sess.Instruments = id.Instruments()
	}
	if sess.ID == "" {
		sess.ID = sess.StartedAt.Format("20060102-150405") + "_" + safe(sess.Product)
	}
	specs, err := app.Prepare(sess, setup)
	if err != nil {
		return err
	}
	sess.Specs = specs
	sess.Records = nil

	dir, err := output.Prepare(e.root, sess)
	if err != nil {
		return err
	}
	sess.OutputDir = dir

	e.app, e.conn, e.sess, e.dir = app, conn, sess, dir
	e.queue = e.queue[:0]
	for i := range specs {
		e.queue = append(e.queue, i)
	}
	return e.persistLocked()
}

// Active reports whether a session is in progress.
func (e *Engine) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sess != nil
}

// Session returns the active session (read-only use).
func (e *Engine) Session() *model.Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sess
}

// App returns the active test app (for the web layer to read PointFields etc.).
func (e *Engine) App() testapp.TestApp {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.app
}

// Current returns the spec at the front of the queue, ok=false when none pending.
func (e *Engine) Current() (model.RecordSpec, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentLocked()
}

func (e *Engine) currentLocked() (model.RecordSpec, bool) {
	if e.sess == nil || len(e.queue) == 0 {
		return model.RecordSpec{}, false
	}
	return e.sess.Specs[e.queue[0]], true
}

// Pending is the number of queued captures; Measured is the number of distinct
// point IDs that have at least one record.
func (e *Engine) Progress() (pending, measured, total int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess == nil {
		return 0, 0, 0
	}
	seen := map[string]bool{}
	for _, r := range e.sess.Records {
		seen[r.ID] = true
	}
	return len(e.queue), len(seen), len(e.sess.Specs)
}

// Connector exposes the active connector.
func (e *Engine) Connector() connector.Connector {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.conn
}

// PrepareStep forces the current spec's device settings onto the connector.
func (e *Engine) PrepareStep() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	spec, ok := e.currentLocked()
	if !ok {
		return nil
	}
	if len(spec.DeviceSettings) == 0 || !e.conn.Capability().CanApply {
		return nil
	}
	return e.conn.Apply(spec.DeviceSettings)
}

// ReadDevice reads the channels the current spec's device fields reference.
func (e *Engine) ReadDevice() ([]connector.Reading, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	spec, ok := e.currentLocked()
	if !ok {
		return nil, fmt.Errorf("engine: no current step")
	}
	if !e.conn.Capability().CanRead {
		return nil, nil
	}
	var chans []string
	for _, f := range spec.Fields {
		if f.Source == model.SourceDevice && f.DeviceChannel != "" {
			chans = append(chans, f.DeviceChannel)
		}
	}
	return e.conn.Read(chans)
}

// Commit builds a timestamped record for the current spec, marks any prior
// records for the same point ID superseded, appends the new attempt, pops the
// queue, and persists. Records are never overwritten.
func (e *Engine) Commit(values map[string]model.Value, attachments []model.Attachment) (model.Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	spec, ok := e.currentLocked()
	if !ok {
		return model.Record{}, fmt.Errorf("engine: nothing to capture")
	}
	ts := e.now().UTC()

	attempt := 1
	for i := range e.sess.Records {
		if e.sess.Records[i].ID == spec.ID {
			attempt++
			e.sess.Records[i].Superseded = true
		}
	}

	rec := model.Record{
		ID:          spec.ID,
		Title:       spec.Title,
		Values:      values,
		CapturedAt:  ts,
		Marker:      spec.Marker,
		Verdict:     model.VerdictPending,
		Attachments: attachments,
		Attempt:     attempt,
	}
	for i := range rec.Attachments {
		if rec.Attachments[i].Time.IsZero() {
			rec.Attachments[i].Time = ts
		}
	}
	if err := e.app.Evaluate(spec, &rec); err != nil {
		return model.Record{}, err
	}
	e.sess.Records = append(e.sess.Records, rec)
	e.queue = e.queue[1:] // pop front
	if err := e.persistLocked(); err != nil {
		return rec, err
	}
	return rec, nil
}

// AddPoint builds a new spec from the app's point params and enqueues it at the
// back of the pending queue (incremental point added during the run).
func (e *Engine) AddPoint(params map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess == nil {
		return fmt.Errorf("engine: no active session")
	}
	spec, err := e.app.NewPoint(e.sess, params)
	if err != nil {
		return err
	}
	e.sess.Specs = append(e.sess.Specs, spec)
	e.queue = append(e.queue, len(e.sess.Specs)-1)
	return e.persistLocked()
}

// Redo re-enqueues an existing point (by spec ID) at the FRONT so it is
// re-measured next; the prior record is retained and will be superseded on the
// next commit.
func (e *Engine) Redo(specID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess == nil {
		return fmt.Errorf("engine: no active session")
	}
	for i := range e.sess.Specs {
		if e.sess.Specs[i].ID == specID {
			e.queue = append([]int{i}, e.queue...)
			return nil
		}
	}
	return fmt.Errorf("engine: unknown point %q", specID)
}

// Finish writes final outputs and turns the source output off if possible.
func (e *Engine) Finish() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess == nil {
		return fmt.Errorf("engine: no active session")
	}
	if e.conn.Capability().CanApply {
		_ = e.conn.Apply(map[string]string{"output": "off"})
	}
	e.sess.Closed = true
	return e.persistLocked()
}

// Root returns the output root (used to find resumable sessions).
func (e *Engine) Root() string { return e.root }

// Suspend persists the current state, turns the source output off, releases the
// connector, and detaches the in-memory session WITHOUT closing it — so it
// remains resumable. This is the "pause" action; closing the app does the same.
func (e *Engine) Suspend() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess == nil {
		return nil
	}
	if e.conn.Capability().CanApply {
		_ = e.conn.Apply(map[string]string{"output": "off"})
	}
	err := e.persistLocked()
	_ = e.conn.Close()
	e.sess, e.app, e.conn = nil, nil, nil
	e.queue = e.queue[:0]
	return err
}

// Resume re-attaches the engine to a previously-persisted, un-closed session and
// rebuilds the pending queue from the specs that have no record yet. The records
// captured before the interruption are preserved.
func (e *Engine) Resume(app testapp.TestApp, conn connector.Connector, sess *model.Session) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.app, e.conn, e.sess = app, conn, sess
	e.dir = sess.OutputDir
	if e.dir == "" {
		e.dir = output.SessionDir(e.root, sess)
	}
	if id, ok := conn.(connector.Identifiable); ok {
		if insts := id.Instruments(); len(insts) > 0 {
			sess.Instruments = insts // refresh from the reconnected instruments
		}
	}
	measured := map[string]bool{}
	for _, r := range sess.Records {
		measured[r.ID] = true
	}
	e.queue = e.queue[:0]
	for i, spec := range sess.Specs {
		if !measured[spec.ID] {
			e.queue = append(e.queue, i)
		}
	}
	return e.persistLocked()
}

// MergeAmbient merges a parsed AZ logger export into the active session by
// timestamp and re-writes the outputs. tol is the max match gap.
func (e *Engine) MergeAmbient(log *ambient.Log, tol time.Duration) (ambient.MergeResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess == nil {
		return ambient.MergeResult{}, fmt.Errorf("engine: no active session")
	}
	res := ambient.Merge(e.sess, log, tol)
	return res, e.persistLocked()
}

func (e *Engine) persistLocked() error {
	e.sess.UpdatedAt = e.now().UTC()
	return output.Write(e.dir, e.sess, e.app)
}

func safe(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "session"
	}
	return string(out)
}
