// Package web serves the local capture UI. It is intentionally a thin layer over
// the engine: setup screen → per-record capture loop (advanced by keystroke) →
// finish. Templates and static assets are embedded so the whole tool ships as a
// single cross-platform binary.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MGZ-LLC/CERTOOL/internal/ambient"
	"github.com/MGZ-LLC/CERTOOL/internal/connector"
	"github.com/MGZ-LLC/CERTOOL/internal/engine"
	"github.com/MGZ-LLC/CERTOOL/internal/model"
	"github.com/MGZ-LLC/CERTOOL/internal/output"
	"github.com/MGZ-LLC/CERTOOL/internal/testapp"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server wires the engine to HTTP handlers.
type Server struct {
	eng  *engine.Engine
	tmpl *template.Template
}

// New builds the server, parsing embedded templates.
func New(eng *engine.Engine) (*Server, error) {
	funcs := template.FuncMap{
		"fmtExpected": fmtExpected,
	}
	tmpl, err := template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{eng: eng, tmpl: tmpl}, nil
}

// Handler returns the configured mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/setup", s.handleSetup)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/resume", s.handleResume)
	mux.HandleFunc("/suspend", s.handleSuspend)
	mux.HandleFunc("/run", s.handleRun)
	mux.HandleFunc("/read", s.handleRead)
	mux.HandleFunc("/apply", s.handleApply)
	mux.HandleFunc("/commit", s.handleCommit)
	mux.HandleFunc("/addpoint", s.handleAddPoint)
	mux.HandleFunc("/redo", s.handleRedo)
	mux.HandleFunc("/import-ambient", s.handleImportAmbient)
	mux.HandleFunc("/finish", s.handleFinish)
	return mux
}

// ---- index / setup ----

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// A *closed* session left in memory must not trap us on /run.
	if s.eng.Active() && !s.eng.Session().Closed {
		http.Redirect(w, r, "/run", http.StatusSeeOther)
		return
	}
	data := map[string]any{"Apps": testapp.List()}
	var resumes []map[string]any
	for _, sess := range output.Resumables(s.eng.Root()) {
		resumes = append(resumes, map[string]any{
			"Product":  sess.Product,
			"TestApp":  sess.TestApp,
			"Measured": distinctRecords(sess),
			"Pending":  len(sess.Specs) - distinctRecords(sess),
			"When":     sess.UpdatedAt.Format("2006-01-02 15:04"),
			"ID":       sess.ID,
		})
	}
	data["Resumes"] = resumes
	s.render(w, "index.html", data)
}

func distinctRecords(sess *model.Session) int {
	seen := map[string]bool{}
	for _, rec := range sess.Records {
		seen[rec.ID] = true
	}
	return len(seen)
}

// handleResume re-attaches the engine to the most recent un-closed session,
// restoring all its setup fields and records so nothing is retyped.
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	if s.eng.Active() && !s.eng.Session().Closed {
		http.Redirect(w, r, "/run", http.StatusSeeOther)
		return
	}
	var sess *model.Session
	if id := r.URL.Query().Get("id"); id != "" {
		for _, cand := range output.Resumables(s.eng.Root()) {
			if cand.ID == id {
				sess = cand
				break
			}
		}
		if sess == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	} else if latest, ok := output.LatestResumable(s.eng.Root()); ok {
		sess = latest
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	app, err := testapp.Get(sess.TestApp)
	if err != nil {
		http.Error(w, "resume: "+err.Error(), http.StatusInternalServerError)
		return
	}
	conn, err := connector.New(sess.Connector, sess.ConnectorAddr)
	if err != nil {
		conn, _ = connector.New("manual", "")
	}
	connectErr := conn.Connect()
	if err := s.eng.Resume(app, conn, sess); err != nil {
		http.Error(w, "resume: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if connectErr != nil {
		http.Redirect(w, r, "/run?connect_error="+urlSafe(connectErr.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/run", http.StatusSeeOther)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	appID := r.URL.Query().Get("app")
	app, err := testapp.Get(appID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.render(w, "setup.html", map[string]any{
		"App":        app.Info(),
		"SetupFields": app.SetupFields(),
		"Connectors": connector.IDs(),
	})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	appID := r.FormValue("app")
	app, err := testapp.Get(appID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	connID := r.FormValue("connector")
	if connID == "" {
		connID = "manual"
	}
	conn, err := connector.New(connID, r.FormValue("addr"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Best-effort connect; a failure falls back to manual entry with a banner.
	connectErr := conn.Connect()

	sess := &model.Session{
		Title:         r.FormValue("title"),
		Product:       r.FormValue("product"),
		Serial:        r.FormValue("serial"),
		ConfigRef:     r.FormValue("config_ref"),
		WiringRef:     r.FormValue("wiring_ref"),
		Operator:      r.FormValue("operator"),
		Witness:       r.FormValue("witness"),
		Location:      r.FormValue("location"),
		ConnectorAddr: r.FormValue("addr"),
	}
	if sess.Title == "" {
		sess.Title = app.Info().Name + " — " + sess.Product
	}

	setup := map[string]string{}
	for _, f := range app.SetupFields() {
		setup[f.Key] = r.FormValue("setup_" + f.Key)
	}

	if err := s.eng.Start(app, conn, sess, setup); err != nil {
		http.Error(w, "start: "+err.Error(), http.StatusBadRequest)
		return
	}
	if connectErr != nil {
		// Non-fatal: engine runs with the connector in unavailable state.
		http.Redirect(w, r, "/run?connect_error="+urlSafe(connectErr.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/run", http.StatusSeeOther)
}

// ---- run loop ----

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if !s.eng.Active() || s.eng.Session().Closed {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	spec, hasCurrent := s.eng.Current()
	if hasCurrent {
		// Force instrument settings for this step (best-effort).
		_ = s.eng.PrepareStep()
	}
	pending, measured, total := s.eng.Progress()
	conn := s.eng.Connector()
	cap := conn.Capability()

	s.render(w, "run.html", map[string]any{
		"Session":      s.eng.Session(),
		"Spec":         spec,
		"HasCurrent":   hasCurrent,
		"Pending":      pending,
		"Measured":     measured,
		"Total":        total,
		"PointFields":  s.eng.App().PointFields(),
		"Connector":    conn.Name(),
		"CanRead":      cap.CanRead && conn.Available(),
		"CanApply":     cap.CanApply && conn.Available(),
		"Available":    conn.Available(),
		"ConnectError": r.URL.Query().Get("connect_error"),
	})
}

// handleRead triggers a device read and returns readings as JSON keyed by channel.
func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	readings, err := s.eng.ReadDevice()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	out := map[string]float64{}
	for _, rd := range readings {
		out[rd.Channel] = rd.Value
	}
	writeJSON(w, http.StatusOK, map[string]any{"readings": out})
}

// handleApply re-forces the current step's device settings.
func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	err := s.eng.PrepareStep()
	msg := "applied"
	if err != nil {
		msg = err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": msg})
}

// handleCommit parses the submitted field values per the current spec, adds any
// photo attachment marker, commits the record, and returns to /run.
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	spec, ok := s.eng.Current()
	if !ok {
		http.Redirect(w, r, "/run", http.StatusSeeOther)
		return
	}
	values := map[string]model.Value{}
	for _, f := range spec.Fields {
		raw := strings.TrimSpace(r.FormValue("f_" + f.Key))
		v := model.Value{Key: f.Key, Source: f.Source}
		switch f.Type {
		case model.FieldNumber:
			if raw == "" {
				continue
			}
			n, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				continue
			}
			v.Num = &n
		case model.FieldBool:
			b := raw == "on" || raw == "true" || raw == "yes" || raw == "1"
			v.Bool = &b
		default:
			if raw == "" {
				continue
			}
			v.Text = raw
		}
		values[f.Key] = v
	}

	var atts []model.Attachment
	if r.FormValue("photo") != "" {
		atts = append(atts, model.Attachment{Kind: "photo", Note: r.FormValue("photo_note")})
	}

	if _, err := s.eng.Commit(values, atts); err != nil {
		http.Error(w, "commit: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/run", http.StatusSeeOther)
}

// handleAddPoint adds a bonding point during the run from the app's PointFields.
func (s *Server) handleAddPoint(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	params := map[string]string{}
	for _, f := range s.eng.App().PointFields() {
		if f.Type == model.FieldBool {
			params[f.Key] = r.FormValue("p_" + f.Key) // "on" when checked
		} else {
			params[f.Key] = strings.TrimSpace(r.FormValue("p_" + f.Key))
		}
	}
	if err := s.eng.AddPoint(params); err != nil {
		http.Error(w, "add point: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/run", http.StatusSeeOther)
}

// handleRedo re-queues an existing point for re-measurement (append-only).
func (s *Server) handleRedo(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}
	if err := s.eng.Redo(id); err != nil {
		http.Error(w, "redo: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/run", http.StatusSeeOther)
}

// handleSuspend pauses the active session (kept resumable) and returns home.
func (s *Server) handleSuspend(w http.ResponseWriter, r *http.Request) {
	if err := s.eng.Suspend(); err != nil {
		http.Error(w, "suspend: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleImportAmbient accepts an uploaded AZ 88163 export and merges it into the
// active session by timestamp, then returns to the finish page.
func (s *Server) handleImportAmbient(w http.ResponseWriter, r *http.Request) {
	if !s.eng.Active() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log, err := ambient.Parse(data)
	if err != nil {
		http.Redirect(w, r, "/finish?ambient="+urlSafe("import failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	res, err := s.eng.MergeAmbient(log, 15*time.Minute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	msg := fmt.Sprintf("AZ %s: matched ambient to %d of %d records", log.Serial, res.Matched, res.Total)
	http.Redirect(w, r, "/finish?ambient="+urlSafe(msg), http.StatusSeeOther)
}

func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request) {
	if !s.eng.Active() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := s.eng.Finish(); err != nil {
		http.Error(w, "finish: "+err.Error(), http.StatusInternalServerError)
		return
	}
	sess := s.eng.Session()
	var instructions []string
	if fin, ok := s.eng.App().(testapp.Finisher); ok {
		instructions = fin.FinishInstructions()
	}
	s.render(w, "done.html", map[string]any{
		"Session":      sess,
		"Records":      sess.Records,
		"Instructions": instructions,
		"AmbientMsg":   r.URL.Query().Get("ambient"),
	})
}

// ---- helpers ----

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fmtExpected(f model.FieldSpec) string {
	if f.Expected != nil {
		return strconv.FormatFloat(*f.Expected, 'f', -1, 64)
	}
	return f.ExpectedText
}

func urlSafe(s string) string { return strings.ReplaceAll(s, " ", "+") }

// ensure fmt import is used even if handlers are trimmed later.
var _ = fmt.Sprintf
