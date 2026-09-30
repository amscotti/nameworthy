package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/amscotti/nameworthy/internal/audit"
	"github.com/amscotti/nameworthy/internal/jev"
)

type fakeJudge struct {
	err           error
	calls         int
	lastState     map[string]any
	lastQuestions map[string]jev.Question
	answers       map[string]jev.Answer
	deadline      time.Time
}

func (f *fakeJudge) Ask(ctx context.Context, state map[string]any, questions map[string]jev.Question) (jev.Response, error) {
	f.calls++
	f.lastState, f.lastQuestions = state, questions
	f.deadline, _ = ctx.Deadline()
	if f.err != nil {
		return jev.Response{}, f.err
	}
	answers := fakeAnswers()
	if f.answers != nil {
		answers = f.answers
	}
	if _, asked := questions[audit.KeyFit]; !asked {
		delete(answers, audit.KeyFit) // mirror reality: fit is only asked with race/class
	}
	return jev.Response{
		Model:   "jev-test",
		Answers: answers,
		Usage:   jev.Usage{InputTokens: 42},
	}, nil
}

func fakeAnswers() map[string]jev.Answer {
	questions := audit.Questions("", "")
	score := func(key string, raw float64, topIdx string) jev.Answer {
		c := 0.6
		probs, legend := map[string]float64{}, map[string]string{}
		for i, level := range questions[key].Criteria.([]string) {
			index := fmt.Sprint(i)
			legend[index] = level
			probs[index] = 0
		}
		probs[topIdx] = 1
		return jev.Answer{Type: jev.TypeScore, Score: &raw,
			Probabilities: probs, Legend: legend, Confidence: &c}
	}
	noul := func(v float64) jev.Answer {
		return jev.Answer{Type: jev.TypeNoul, Noul: &v}
	}
	choice := func(key, pick string, probs map[string]float64) jev.Answer {
		c := 0.7
		for option := range questions[key].Criteria.(map[string]*string) {
			if _, ok := probs[option]; !ok {
				probs[option] = 0
			}
		}
		return jev.Answer{Type: jev.TypeChoice, Choice: &pick, Probabilities: probs, Confidence: &c}
	}
	return map[string]jev.Answer{
		audit.KeyLoreFit:    score(audit.KeyLoreFit, 3.6, "4"),    // textbook
		audit.KeyHumor:      score(audit.KeyHumor, 1.2, "1"),      // a smirk
		audit.KeyCreativity: score(audit.KeyCreativity, 2.4, "2"), // solid
		audit.KeyPronounce:  score(audit.KeyPronounce, 3.2, "3"),  // smooth
		audit.KeyImmersion:  score(audit.KeyImmersion, 3.4, "4"),  // pure RP
		audit.KeyEffort:     score(audit.KeyEffort, 2.8, "3"),     // crafted
		audit.KeyVulgar:     noul(0.05),
		audit.KeyReportable: noul(0.1),
		audit.KeyFit:        noul(0.8),
		audit.KeyGrade:      choice(audit.KeyGrade, "S", map[string]float64{"S": 0.4, "A": 0.3, "B": 0.2, "C": 0.1}),
		audit.KeyArchetype:  choice(audit.KeyArchetype, "serious_rp", map[string]float64{"serious_rp": 0.5, "lore_master": 0.3, "generic": 0.2}),
	}
}

func postAudit(t *testing.T, s *Server, endpoint, first, last, race, class string) *http.Response {
	t.Helper()
	form := url.Values{
		"first_name": {first}, "last_name": {last}, "race": {race}, "class": {class},
	}
	req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec.Result()
}

func TestAuditHTMLFlow(t *testing.T) {
	judge := &fakeJudge{}
	s := New(judge)

	resp := postAudit(t, s, "/api/audit", "Thalen", "Brightvale", "Human", "Paladin")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		"Thalen Brightvale", // the name under the lens
		">S<",               // the grade crest
		"Serious RP",        // archetype
		"Lore Fit", "Humor", "Creativity", "Pronounceability", "RP Immersion", "Craft",
		"Vulgarity", "Policy Risk", "Character Fit",
		"textbook", // top level of lore fit
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}

	if judge.lastState["first_name"] != "Thalen" || judge.lastState["race"] != "Human" || judge.lastState["faction"] != "Alliance" {
		t.Errorf("state identity wrong: %v", judge.lastState)
	}
	if len(judge.lastQuestions) != 11 {
		t.Errorf("questions = %d, want 11 with race+class", len(judge.lastQuestions))
	}
}

func TestInvalidCharacterDoesNotCallJudge(t *testing.T) {
	judge := &fakeJudge{}
	s := New(judge)
	for _, pair := range [][2]string{
		{"Tauren", "Rogue"}, {"Night Elf", "Mage"}, {"Skyborne (High Order)", "Shaman"},
		{"Skyborne (Windshaper)", "Mage"}, {"Blood Elf", "Paladin"}, {"Human", "Death Knight"},
	} {
		resp := postAudit(t, s, "/api/audit?format=json", "Thalen", "Brightvale", pair[0], pair[1])
		if resp.StatusCode != http.StatusBadRequest || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			t.Errorf("%v: status %d, content-type %s", pair, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		var body map[string]string
		if err := json.Unmarshal([]byte(readBody(t, resp)), &body); err != nil || body["error"] == "" {
			t.Errorf("%v: invalid JSON error: %v / %v", pair, body, err)
		}
	}
	if judge.calls != 0 {
		t.Fatalf("invalid combinations cost %d model calls", judge.calls)
	}
}

func TestInputBoundsAndJSONParsing(t *testing.T) {
	judge := &fakeJudge{}
	s := New(judge)
	name := strings.Repeat("é", maxNameRunes)
	resp := postAudit(t, s, "/api/audit?format=json", name, "雪", "", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(readBody(t, resp), name) {
		t.Fatal("multibyte names must survive unchanged at the character limit")
	}
	resp = postAudit(t, s, "/api/audit", name+"é", "雪", "", "")
	readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || judge.calls != 1 {
		t.Fatal("overlong names must be rejected before a paid call")
	}
	for _, tc := range []struct {
		contentType, body string
		status            int
	}{
		{"application/json", `{"first_name":"A","last_name":"B"} {}`, 400},
		{"application/json", `{"first_name":"A","last_name":"B","typo":true}`, 400},
		{"application/json", `{"first_name":"A","last_name":"B","race":false}`, 400},
		{"application/json", strings.Repeat(" ", maxBodyBytes+1), 413},
		{"application/x-www-form-urlencoded", "first_name=" + strings.Repeat("a", maxBodyBytes+1), 413},
		{"text/plain", "first_name=A&last_name=B", 415},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/audit?format=json", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.contentType)
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s: status = %d, want %d; %s", tc.contentType, rec.Code, tc.status, rec.Body)
		}
		if !json.Valid(rec.Body.Bytes()) {
			t.Error("invalid input must still return JSON to JSON callers")
		}
	}
	if judge.calls != 1 {
		t.Fatalf("malformed input caused extra judge calls: %d", judge.calls)
	}
}

func TestDeadlineAndMissingFit(t *testing.T) {
	judge := &fakeJudge{answers: fakeAnswers()}
	delete(judge.answers, audit.KeyFit)
	s := New(judge)
	start := time.Now()
	resp := postAudit(t, s, "/api/audit?format=json", "Thalen", "Brightvale", "Human", "Hunter")
	if resp.StatusCode != http.StatusBadGateway || !json.Valid([]byte(readBody(t, resp))) {
		t.Fatal("a missing requested fit must fail the audit with a JSON error")
	}
	if left := judge.deadline.Sub(start); left <= 0 || left > auditRequestTimeout+time.Second {
		t.Errorf("judge deadline = %v, want a bounded %v request", left, auditRequestTimeout)
	}
}

func TestAcceptJSONAndUnknownRoute(t *testing.T) {
	s := New(&fakeJudge{err: context.DeadlineExceeded})
	req := httptest.NewRequest(http.MethodPost, "/api/audit", strings.NewReader("first_name=A&last_name=B"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusGatewayTimeout || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("timeout = %d / %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown route = %d, want 404", rec.Code)
	}
}

func TestAuditJSONEnvelope(t *testing.T) {
	s := New(&fakeJudge{})

	resp := postAudit(t, s, "/api/audit?format=json", "Thalen", "Brightvale", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		First     string       `json:"first_name"`
		Last      string       `json:"last_name"`
		Grade     audit.Option `json:"grade"`
		Archetype audit.Option `json:"archetype"`
		Dims      []audit.Dim  `json:"dims"`
		Flags     []audit.Flag `json:"flags"`
		Model     string       `json:"model"`
		LatencyMS int64        `json:"latency_ms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.First != "Thalen" || env.Last != "Brightvale" {
		t.Errorf("identity wrong: %+v", env)
	}
	if env.Grade.ID != "S" || env.Archetype.ID != "serious_rp" {
		t.Errorf("verdict wrong: %+v / %+v", env.Grade, env.Archetype)
	}
	if len(env.Dims) != 6 || len(env.Flags) != 2 { // no race/class → no fit flag
		t.Errorf("dims/flags = %d/%d, want 6/2", len(env.Dims), len(env.Flags))
	}
	if env.Model != "jev-test" || env.LatencyMS < 0 {
		t.Errorf("provenance wrong: %+v", env)
	}
}

func TestMissingNames(t *testing.T) {
	judge := &fakeJudge{}
	s := New(judge)

	for _, tc := range []struct{ first, last string }{
		{"", "Brightvale"}, {"Thalen", ""}, {"", ""},
	} {
		resp := postAudit(t, s, "/api/audit", tc.first, tc.last, "", "")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q/%q: status = %d, want 400", tc.first, tc.last, resp.StatusCode)
		}
		if !strings.Contains(body, "needs both a first name and a surname") {
			t.Errorf("%q/%q: body missing the friendly message", tc.first, tc.last)
		}
	}
	if judge.calls != 0 {
		t.Error("judge must not be consulted on invalid input")
	}
}

func TestJudgeErrorRendersCard(t *testing.T) {
	s := New(&fakeJudge{err: fmt.Errorf("spirits unreachable")})

	resp := postAudit(t, s, "/api/audit", "Thalen", "Brightvale", "", "")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	if !strings.Contains(body, "spirits are unreachable") {
		t.Errorf("error card missing: %s", body)
	}
}

func TestRateLimit(t *testing.T) {
	judge := &fakeJudge{}
	s := New(judge)

	for i := 0; i < rateLimitMax; i++ {
		resp := postAudit(t, s, "/api/audit", "Thalen", "Brightvale", "", "")
		readBody(t, resp)
	}
	resp := postAudit(t, s, "/api/audit", "Thalen", "Brightvale", "", "")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
	if !strings.Contains(body, "try again in a moment") {
		t.Errorf("rate-limit card missing: %s", body)
	}
	if judge.calls != rateLimitMax {
		t.Errorf("judge calls = %d, want %d", judge.calls, rateLimitMax)
	}
}

func TestPageAndHealth(t *testing.T) {
	s := New(&fakeJudge{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("page status = %d", rec.Code)
	}
	for _, want := range []string{
		"Is your name worthy?", `id="first_name"`, `id="last_name"`,
		"https://unpkg.com/htmx.org@4.0.0/dist/htmx.min.js",
		"https://unpkg.com/alpinejs@3.17.4/dist/cdn.min.js",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("health = %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("missing baseline security headers: %v", rec.Header())
	}

	for _, asset := range []string{"/static/app.css", "/static/app.js"} {
		rec = httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("GET %s = %d (%d bytes)", asset, rec.Code, rec.Body.Len())
		}
	}
}

func TestAuditValidJSONRequestBody(t *testing.T) {
	judge := &fakeJudge{}
	s := New(judge)

	payload := `{"first_name":"  Mira ","last_name":" Windweaver ","race":"skyborne (windshaper)","class":"shaman"}`
	req := httptest.NewRequest(http.MethodPost, "/api/audit", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got struct {
		First string `json:"first_name"`
		Last  string `json:"last_name"`
		Race  string `json:"race"`
		Class string `json:"class"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.First != "Mira" || got.Last != "Windweaver" || got.Race != "Skyborne (Windshaper)" || got.Class != "Shaman" {
		t.Errorf("canonical JSON response = %+v", got)
	}
	if judge.lastState["faction"] != "Horde" {
		t.Errorf("expected Horde faction context for Windshaper, got %v", judge.lastState["faction"])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJevJudgeAndUpstreamErrorMappings(t *testing.T) {
	// Full JevJudge round-trip: serialize real audit questions and fold a real JSON response.
	okBody, err := json.Marshal(jev.Response{
		Model:   "jev-1.13.0",
		Answers: fakeAnswers(),
		Usage:   jev.Usage{InputTokens: 512, OutputTokens: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	okClient := jev.NewClient("test-key",
		jev.WithBaseURL("http://jev.test/v1/systemone"),
		jev.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(okBody))),
			}, nil
		})}),
	)
	okResp := postAudit(t, New(JevJudge{Client: okClient}), "/api/audit?format=json", "Thalen", "Brightvale", "Human", "Paladin")
	if okResp.StatusCode != http.StatusOK || !strings.Contains(readBody(t, okResp), `"model":"jev-1.13.0"`) {
		t.Fatalf("full JevJudge round-trip failed: status %d", okResp.StatusCode)
	}

	// Keyless server serves 503 with configuration guidance on audit requests.
	keyless := New(JevJudge{Client: jev.NewClient("")})
	resp := postAudit(t, keyless, "/api/audit?format=json", "Thalen", "Brightvale", "", "")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "TYPESAFE_API_KEY") {
		t.Fatalf("keyless audit = %d %s, want 503 mentioning TYPESAFE_API_KEY", resp.StatusCode, body)
	}

	// Upstream Jev HTTP statuses map to distinct, user-friendly 502 messages.
	for _, tc := range []struct {
		upstream int
		wantSub  string
	}{
		{http.StatusUnauthorized, "API key was rejected"},
		{http.StatusTooManyRequests, "busy or rate limiting"},
		{http.StatusUnprocessableEntity, "could not read the audit questions"},
	} {
		client := jev.NewClient("test-key",
			jev.WithBaseURL("http://jev.test/v1/systemone"),
			jev.WithAttempts(1),
			jev.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tc.upstream,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":"upstream error"}`)),
				}, nil
			})}),
		)
		srv := New(JevJudge{Client: client})
		r := postAudit(t, srv, "/api/audit?format=json", "Thalen", "Brightvale", "", "")
		got := readBody(t, r)
		if r.StatusCode != http.StatusBadGateway || !strings.Contains(got, tc.wantSub) {
			t.Errorf("upstream %d -> %d %s, want 502 containing %q", tc.upstream, r.StatusCode, got, tc.wantSub)
		}
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(data)
}
