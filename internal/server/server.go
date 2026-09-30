// Package server hosts the one-page name auditor: a WoW: Forever name in,
// a ranked verdict out.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amscotti/nameworthy/internal/audit"
	"github.com/amscotti/nameworthy/internal/character"
	"github.com/amscotti/nameworthy/internal/jev"
	"github.com/amscotti/nameworthy/web"
)

const (
	maxNameRunes        = 24 // tool input bound, not a claim about in-game limits
	maxBodyBytes        = 16 << 10
	auditRequestTimeout = 15 * time.Second
)

// Judge evaluates a name audit in a single parallel System One call.
type Judge interface {
	Ask(ctx context.Context, state map[string]any, questions map[string]jev.Question) (jev.Response, error)
}

// JevJudge asks the TypeSafe System One model.
type JevJudge struct {
	Client *jev.Client
}

// Ask evaluates the question set against the state in one round trip.
func (j JevJudge) Ask(ctx context.Context, state map[string]any, questions map[string]jev.Question) (jev.Response, error) {
	resp, err := j.Client.SystemOne(ctx, state, questions)
	if err != nil {
		return jev.Response{}, err
	}
	return *resp, nil
}

// Server serves the page and the audit API.
type Server struct {
	judge   Judge
	tmpl    *template.Template
	static  fs.FS
	limiter *rateLimiter
	now     func() time.Time
}

// New builds the server around a Judge.
func New(judge Judge) *Server {
	tmpl := template.Must(template.New("").
		Funcs(template.FuncMap{"pct": pct}).
		ParseFS(web.FS, "templates/*.html"))
	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		panic(fmt.Sprintf("server: embedded static fs: %v", err))
	}
	return &Server{
		judge:   judge,
		tmpl:    tmpl,
		static:  static,
		limiter: newRateLimiter(rateLimitMax, rateLimitWindow),
		now:     time.Now,
	}
}

// Routes wires the mux: the page, the audit API, static assets, health.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.static))))
	// /health, not /healthz: Cloud Run's Google Front End reserves paths
	// ending in "z" (/healthz, /livez, /readyz) and answers them with its
	// own 404 before the request ever reaches the container.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /{$}", s.handlePage)
	mux.HandleFunc("POST /api/audit", s.handleAudit)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		mux.ServeHTTP(w, r)
	})
}

// envelope is the app JSON shape: the inputs echoed plus the verdict.
type envelope struct {
	First string `json:"first_name"`
	Last  string `json:"last_name"`
	Race  string `json:"race,omitempty"`
	Class string `json:"class,omitempty"`
	audit.Audit
	Model     string     `json:"model,omitempty"`
	Usage     *jev.Usage `json:"usage,omitempty"`
	LatencyMS int64      `json:"latency_ms,omitempty"`
}

func (s *Server) handlePage(w http.ResponseWriter, _ *http.Request) {
	catalog := character.All()
	data, err := json.Marshal(catalog)
	if err != nil {
		slog.Error("encode character catalog", "err", err)
		http.Error(w, "could not open the ledger", http.StatusInternalServerError)
		return
	}
	s.render(w, http.StatusOK, "page", map[string]any{
		"Catalog": string(data), "Races": catalog.Races, "Classes": catalog.Classes,
	})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	w.Header().Set("Cache-Control", "no-store")
	first, last, race, class, err := parseInput(r)
	if err != nil {
		status := http.StatusBadRequest
		var invalid *inputError
		if errors.As(err, &invalid) {
			status = invalid.status
		}
		s.renderErr(w, r, status, err.Error())
		return
	}

	// Every audit spends paid API tokens: small per-IP window, humans first.
	if ok, retryAfter := s.limiter.allow(clientIP(r)); !ok {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
		s.renderErr(w, r, http.StatusTooManyRequests,
			"the ledger needs a breath — try again in a moment.")
		return
	}

	state := map[string]any{
		"first_name": first,
		"last_name":  last,
		"context":    "World of Warcraft: Forever, Classic-era Azeroth with nine classes; a separate continuity from modern retail Warcraft. Judge first_name and last_name together, for fun. This does not check name availability or guarantee Blizzard policy compliance.",
	}
	if race != "" {
		state["race"] = race
		if info, ok := character.ByRace(race); ok {
			state["faction"] = info.Faction
			state["race_context"] = info.Description
		}
	}
	if class != "" {
		state["class"] = class
	}

	start := s.now()
	ctx, cancel := context.WithTimeout(r.Context(), auditRequestTimeout)
	defer cancel()
	questions := audit.Questions(race, class)
	resp, err := s.judge.Ask(ctx, state, questions)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := resp.ValidateFor(questions); err != nil {
		s.fail(w, r, err)
		return
	}
	verdict, err := audit.FromAnswers(resp.Answers)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	env := envelope{
		First: first, Last: last, Race: race, Class: class,
		Audit:     verdict,
		Model:     resp.Model,
		Usage:     &resp.Usage,
		LatencyMS: s.now().Sub(start).Milliseconds(),
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(env); err != nil {
			slog.Error("encode response", "err", err)
		}
		return
	}
	raceInfo, _ := character.ByRace(race)
	classInfo, _ := character.ByClass(class)
	s.render(w, http.StatusOK, "results", map[string]any{"A": env, "Race": raceInfo, "Class": classInfo})
}

// fail renders the error card with an honest status code (HTMX 4 swaps
// 4xx/5xx by default, so the partial lands either way).
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("audit", "err", err)
	status, message := http.StatusBadGateway, "the spirits are unreachable. Try auditing this name again."
	var apiErr *jev.APIError
	switch {
	case errors.Is(err, jev.ErrMissingAPIKey):
		status, message = http.StatusServiceUnavailable, "Auditing needs a TypeSafe API key. Set TYPESAFE_API_KEY and restart the app."
	case errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusGatewayTimeout, "The audit timed out. Try auditing this name again."
	case errors.As(err, &apiErr):
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			message = "The TypeSafe API key was rejected. Check TYPESAFE_API_KEY and restart the app."
		case http.StatusTooManyRequests, 529:
			message = "TypeSafe is busy or rate limiting requests. Wait a moment, then audit again."
		case http.StatusUnprocessableEntity:
			message = "TypeSafe could not read the audit questions. Check the server logs."
		}
	}
	s.renderErr(w, r, status, message)
}

func wantsJSON(r *http.Request) bool {
	return r.URL.Query().Get("format") == "json" || strings.Contains(r.Header.Get("Accept"), "application/json")
}

func (s *Server) renderErr(w http.ResponseWriter, r *http.Request, status int, message string) {
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
			slog.Error("encode error response", "err", err)
		}
		return
	}
	s.render(w, status, "error", map[string]any{"Message": message})
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	var body bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&body, name, data); err != nil {
		slog.Error("render template", "template", name, "err", err)
		http.Error(w, "could not open the ledger", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body.Bytes()); err != nil {
		slog.Error("write template", "err", err)
	}
}

// parseInput pulls the identity from a form post or a JSON body. First name
// and surname are both required (Forever requires surnames — so does the
// ledger); race and class are optional. Overlong names are rejected, never
// silently changed into a different identity. The handler bounds every body.
func parseInput(r *http.Request) (first, last, race, class string, err error) {
	contentType := r.Header.Get("Content-Type")
	mediaType := ""
	if contentType != "" {
		mediaType, _, err = mime.ParseMediaType(contentType)
		if err != nil {
			return "", "", "", "", fmt.Errorf("the ledger could not read that content type")
		}
	}
	switch mediaType {
	case "application/json":
		var body struct {
			First string `json:"first_name"`
			Last  string `json:"last_name"`
			Race  string `json:"race"`
			Class string `json:"class"`
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return "", "", "", "", bodyError(err)
		}
		if !utf8.Valid(data) {
			return "", "", "", "", fmt.Errorf("send names as valid UTF-8 text")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			return "", "", "", "", fmt.Errorf(`the ledger could not read that. Send {"first_name":…,"last_name":…,"race":…,"class":…}`)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return "", "", "", "", fmt.Errorf("send one JSON object per audit")
		}
		first, last, race, class = body.First, body.Last, body.Race, body.Class
	case "", "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return "", "", "", "", bodyError(err)
		}
		first = r.PostFormValue("first_name")
		last = r.PostFormValue("last_name")
		race = r.PostFormValue("race")
		class = r.PostFormValue("class")
	default:
		return "", "", "", "", &inputError{http.StatusUnsupportedMediaType, "send an encoded form or an application/json object"}
	}

	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	race, class = strings.TrimSpace(race), strings.TrimSpace(class)
	if first == "" || last == "" {
		return "", "", "", "", fmt.Errorf("the ledger needs both a first name and a surname — Forever requires surnames, and so do we")
	}
	for _, name := range []string{first, last} {
		if !utf8.ValidString(name) {
			return "", "", "", "", fmt.Errorf("send names as valid UTF-8 text")
		}
		if utf8.RuneCountInString(name) > maxNameRunes {
			return "", "", "", "", fmt.Errorf("use at most %d characters in each name; the ledger will not shorten your name", maxNameRunes)
		}
	}
	race, class, err = character.Resolve(race, class)
	if err != nil {
		return "", "", "", "", err
	}
	return first, last, race, class, nil
}

type inputError struct {
	status  int
	message string
}

func (e *inputError) Error() string { return e.message }

func bodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &inputError{http.StatusRequestEntityTooLarge, "that request is too large — send just the names and optional character choices"}
	}
	return fmt.Errorf("the ledger could not read that request")
}

func pct(p float64) string {
	return fmt.Sprintf("%.0f%%", p*100)
}
