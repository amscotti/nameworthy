package jev

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// The fixtures below are the exact payloads published in the TypeSafe API
// reference (docs.typesafe.ai/api) and quick start.

const docsNoulResponse = `{"model":"jev-latest","answers":{"is_urgent":{"type":"noul","noul":0.92}},"usage":{"input_tokens":312,"output_tokens":48}}`

const docsChoiceResponse = `{"model":"jev-latest","answers":{"department":{"type":"choice","choice":"technical","probabilities":{"billing":0.08,"technical":0.85,"sales":0.07},"confidence":0.82}},"usage":{"input_tokens":312,"output_tokens":48}}`

const docsScoreResponse = `{"model":"jev-latest","answers":{"frustration":{"type":"score","score":1.6,"legend":{"0":"Calm","1":"Frustrated","2":"Very angry"},"probabilities":{"0":0.05,"1":0.3,"2":0.65},"confidence":0.78}},"usage":{"input_tokens":312,"output_tokens":48}}`

func decodeResponse(t *testing.T, body string) (*Response, error) {
	t.Helper()
	var resp Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	return &resp, resp.Validate()
}

func TestDecodeNoulAnswer(t *testing.T) {
	resp, err := decodeResponse(t, docsNoulResponse)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Model != "jev-latest" {
		t.Errorf("model = %q", resp.Model)
	}
	if resp.Usage.InputTokens != 312 || resp.Usage.OutputTokens != 48 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	ans, ok := resp.Answers["is_urgent"]
	if !ok {
		t.Fatal("missing is_urgent answer")
	}
	if ans.Type != TypeNoul || ans.Noul == nil || *ans.Noul != 0.92 {
		t.Errorf("answer = %+v", ans)
	}
}

func TestDecodeChoiceAnswer(t *testing.T) {
	resp, err := decodeResponse(t, docsChoiceResponse)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	ans := resp.Answers["department"]
	if ans.Type != TypeChoice || ans.Choice == nil || *ans.Choice != "technical" {
		t.Fatalf("answer = %+v", ans)
	}
	if got := ans.Probabilities["billing"]; got != 0.08 {
		t.Errorf("billing probability = %v", got)
	}
	if ans.Confidence == nil || *ans.Confidence != 0.82 {
		t.Errorf("confidence = %v", ans.Confidence)
	}
}

func TestDecodeScoreAnswer(t *testing.T) {
	resp, err := decodeResponse(t, docsScoreResponse)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	ans := resp.Answers["frustration"]
	if ans.Type != TypeScore || ans.Score == nil || *ans.Score != 1.6 {
		t.Fatalf("answer = %+v", ans)
	}
	if ans.Legend["1"] != "Frustrated" {
		t.Errorf("legend = %v", ans.Legend)
	}
	if got := ans.Probabilities["2"]; got != 0.65 {
		t.Errorf("level 2 probability = %v", got)
	}
	if ans.Confidence == nil || *ans.Confidence != 0.78 {
		t.Errorf("confidence = %v", ans.Confidence)
	}
}

func TestDecodeRejectsBadAnswers(t *testing.T) {
	cases := map[string]string{
		"missing type":        `{"model":"m","answers":{"q":{"noul":0.5}},"usage":{}}`,
		"unknown type":        `{"model":"m","answers":{"q":{"type":"vibe","noul":0.5}},"usage":{}}`,
		"noul missing value":  `{"model":"m","answers":{"q":{"type":"noul"}},"usage":{}}`,
		"noul out of range":   `{"model":"m","answers":{"q":{"type":"noul","noul":1.7}},"usage":{}}`,
		"noul negative":       `{"model":"m","answers":{"q":{"type":"noul","noul":-0.1}},"usage":{}}`,
		"choice no probs":     `{"model":"m","answers":{"q":{"type":"choice","choice":"a","confidence":0.5}},"usage":{}}`,
		"choice not in probs": `{"model":"m","answers":{"q":{"type":"choice","choice":"zz","probabilities":{"a":1.0},"confidence":0.5}},"usage":{}}`,
		"probs sum wrong":     `{"model":"m","answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":0.9,"b":0.9},"confidence":0.5}},"usage":{}}`,
		"confidence missing":  `{"model":"m","answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":1.0}},"usage":{}}`,
		"confidence range":    `{"model":"m","answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":1.0},"confidence":1.4}},"usage":{}}`,
		"score no legend":     `{"model":"m","answers":{"q":{"type":"score","score":1.0,"probabilities":{"0":1.0},"confidence":0.5}},"usage":{}}`,
		"score legend thin":   `{"model":"m","answers":{"q":{"type":"score","score":1.0,"legend":{"0":"a"},"probabilities":{"0":1.0},"confidence":0.5}},"usage":{}}`,
		"score level orphan":  `{"model":"m","answers":{"q":{"type":"score","score":1.0,"legend":{"0":"a","1":"b"},"probabilities":{"7":1.0},"confidence":0.5}},"usage":{}}`,
		"score missing value": `{"model":"m","answers":{"q":{"type":"score","legend":{"0":"a","1":"b"},"probabilities":{"0":1.0,"1":0.0},"confidence":0.5}},"usage":{}}`,
		"missing answers":     `{"model":"m","usage":{}}`,
		"missing model":       `{"answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`,
		"missing usage":       `{"model":"m","answers":{"q":{"type":"noul","noul":0.5}}}`,
		"null usage":          `{"model":"m","answers":{"q":{"type":"noul","noul":0.5}},"usage":null}`,
		"negative usage":      `{"model":"m","answers":{"q":{"type":"noul","noul":0.5}},"usage":{"input_tokens":-1}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeResponse(t, body); err == nil {
				t.Fatal("want decode/validation error, got nil")
			}
		})
	}
}

func TestQuestionConstructorsMarshal(t *testing.T) {
	cases := map[string]struct {
		q    Question
		want string
	}{
		"noul": {
			q:    Noul("Does this convey urgency?"),
			want: `{"type":"noul","instructions":"Does this convey urgency?"}`,
		},
		"noul with criteria": {
			q:    NoulWithCriteria("The message is angry", "explicitly angry", "neutral or friendly"),
			want: `{"type":"noul","instructions":"The message is angry","criteria":{"true":"explicitly angry","false":"neutral or friendly"}}`,
		},
		"choice with null detail": {
			q:    Choice("Which team?", map[string]string{"billing": "Payments", "sales": ""}),
			want: `{"type":"choice","instructions":"Which team?","criteria":{"billing":"Payments","sales":null}}`,
		},
		"score": {
			q:    Score("How frustrated?", []string{"Calm", "Frustrated", "Very angry"}),
			want: `{"type":"score","instructions":"How frustrated?","criteria":["Calm","Frustrated","Very angry"]}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.q.Validate(); err != nil {
				t.Fatalf("validate: %v", err)
			}
			got, err := json.Marshal(tc.q)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var gotNorm, wantNorm any
			if err := json.Unmarshal(got, &gotNorm); err != nil {
				t.Fatalf("remarshal: %v", err)
			}
			if err := json.Unmarshal([]byte(tc.want), &wantNorm); err != nil {
				t.Fatalf("want json: %v", err)
			}
			gotJSON, _ := json.Marshal(gotNorm)
			wantJSON, _ := json.Marshal(wantNorm)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("marshal = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestQuestionValidateRejects(t *testing.T) {
	cases := map[string]Question{
		"no instructions":   {Type: TypeNoul},
		"unknown type":      {Type: "vibe", Instructions: "x"},
		"score one level":   Score("x", []string{"only"}),
		"choice empty":      Choice("x", nil),
		"noul bad criteria": {Type: TypeNoul, Instructions: "x", Criteria: []string{"a"}},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			if err := q.Validate(); err == nil {
				t.Fatal("want validation error, got nil")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       ioNopCloser(body),
	}
}

type nopCloser struct{ *strings.Reader }

func ioNopCloser(s string) nopCloser { return nopCloser{strings.NewReader(s)} }

func (nopCloser) Close() error { return nil }

func testClient(rt http.RoundTripper) *Client {
	return NewClient("test-key",
		WithBaseURL("http://jev.test/v1/systemone"),
		WithHTTPClient(&http.Client{Transport: rt}),
		WithBackoff(time.Millisecond),
	)
}

func TestClientRequestShape(t *testing.T) {
	var gotAuth, gotCT, gotMethod, gotURL string
	var gotBody map[string]any
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotMethod = r.Method
		gotURL = r.URL.String()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		return jsonResponse(http.StatusOK, docsNoulResponse), nil
	})

	resp, err := testClient(rt).SystemOne(context.Background(),
		map[string]any{"message": "hello"},
		map[string]Question{"is_urgent": Noul("Does this convey urgency?")},
	)
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if resp.Model != "jev-latest" {
		t.Errorf("model = %q", resp.Model)
	}
	if gotMethod != http.MethodPost || gotURL != "http://jev.test/v1/systemone" {
		t.Errorf("request line = %s %s", gotMethod, gotURL)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody["model"] != Model {
		t.Errorf("body model = %v", gotBody["model"])
	}
	if _, ok := gotBody["questions"].(map[string]any)["is_urgent"]; !ok {
		t.Errorf("body questions = %v", gotBody["questions"])
	}
}

func TestClientStateKinds(t *testing.T) {
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, docsNoulResponse), nil
	})
	c := testClient(rt)
	q := map[string]Question{"is_urgent": Noul("yes?")}

	for name, state := range map[string]any{
		"string": "hello",
		"object": map[string]any{"message": "hi"},
		"array":  []any{"a", "b"},
	} {
		if _, err := c.SystemOne(context.Background(), state, q); err != nil {
			t.Errorf("state kind %s must be accepted: %v", name, err)
		}
	}
	for name, state := range map[string]any{"number": 42, "null": nil, "bool": true} {
		if _, err := c.SystemOne(context.Background(), state, q); err == nil {
			t.Errorf("state kind %s must be rejected", name)
		}
	}
}

func TestClientRetriesOn429And529(t *testing.T) {
	calls := 0
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return jsonResponse(http.StatusTooManyRequests, `{"error":"rate limited"}`), nil
		case 2:
			return jsonResponse(529, `{"error":"overloaded"}`), nil
		default:
			return jsonResponse(http.StatusOK, docsNoulResponse), nil
		}
	})

	if _, err := testClient(rt).SystemOne(context.Background(), "s",
		map[string]Question{"is_urgent": Noul("yes?")}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestClientGivesUpAfterRetries(t *testing.T) {
	calls := 0
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(529, `{"error":"overloaded"}`), nil
	})

	_, err := testClient(rt).SystemOne(context.Background(), "s",
		map[string]Question{"q": Noul("yes?")})
	if err == nil {
		t.Fatal("want error after retries exhausted")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestClientHonorsRetryAfter(t *testing.T) {
	calls := 0
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := jsonResponse(http.StatusTooManyRequests, `{"error":"rate limited"}`)
			resp.Header.Set("Retry-After", "1")
			return resp, nil
		}
		return jsonResponse(http.StatusOK, docsNoulResponse), nil
	})

	start := time.Now()
	if _, err := testClient(rt).SystemOne(context.Background(), "s",
		map[string]Question{"is_urgent": Noul("yes?")}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("elapsed = %v, want >= 1s (Retry-After was ignored)", elapsed)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestClientAttemptsOption(t *testing.T) {
	newOverloaded := func(calls *int) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			*calls++
			return jsonResponse(529, `{"error":"overloaded"}`), nil
		})
	}

	calls := 0
	c := NewClient("test-key",
		WithBaseURL("http://jev.test/v1/systemone"),
		WithHTTPClient(&http.Client{Transport: newOverloaded(&calls)}),
		WithBackoff(time.Millisecond),
		WithAttempts(1),
	)
	if _, err := c.SystemOne(context.Background(), "s",
		map[string]Question{"q": Noul("yes?")}); err == nil {
		t.Fatal("want error after 1 attempt")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}

	// Non-positive attempts clamp to a single try, never zero.
	for _, n := range []int{0, -3} {
		calls = 0
		c := NewClient("test-key",
			WithBaseURL("http://jev.test/v1/systemone"),
			WithHTTPClient(&http.Client{Transport: newOverloaded(&calls)}),
			WithBackoff(time.Millisecond),
			WithAttempts(n),
		)
		if _, err := c.SystemOne(context.Background(), "s",
			map[string]Question{"q": Noul("yes?")}); err == nil {
			t.Fatalf("attempts=%d: want error", n)
		}
		if calls != 1 {
			t.Errorf("attempts=%d: calls = %d, want 1", n, calls)
		}
	}
}

func TestClientNoRetryOnClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusUnprocessableEntity} {
		calls := 0
		rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return jsonResponse(status, `{"error":{"message":"nope"}}`), nil
		})

		_, err := testClient(rt).SystemOne(context.Background(), "s",
			map[string]Question{"q": Noul("yes?")})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: want *APIError, got %v", status, err)
		}
		if apiErr.StatusCode != status || apiErr.Message != "nope" {
			t.Errorf("status %d: apiErr = %+v", status, apiErr)
		}
		if calls != 1 {
			t.Errorf("status %d: calls = %d, want 1", status, calls)
		}
	}
}

func TestClientRejectsBadQuestion(t *testing.T) {
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("no request should be sent for an invalid question")
		return nil, nil
	})
	if _, err := testClient(rt).SystemOne(context.Background(), "s",
		map[string]Question{"q": Score("x", []string{"only"})}); err == nil {
		t.Fatal("want validation error")
	}
}

func TestClientCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err // like http.Transport, refuse work for dead contexts
		}
		return jsonResponse(http.StatusOK, docsNoulResponse), nil
	})
	if _, err := testClient(rt).SystemOne(ctx, "s",
		map[string]Question{"q": Noul("yes?")}); err == nil {
		t.Fatal("want error from canceled context")
	}
}

func TestCappedRetryAfter(t *testing.T) {
	if got := cappedRetryAfter(http.Header{"Retry-After": []string{"2"}}); got != 2*time.Second {
		t.Errorf("cappedRetryAfter(2) = %v, want 2s", got)
	}
	if got := cappedRetryAfter(http.Header{"Retry-After": []string{"3600"}}); got != 30*time.Second {
		t.Errorf("cappedRetryAfter(3600) = %v, want 30s cap", got)
	}
	if got := cappedRetryAfter(http.Header{}); got != 0 {
		t.Errorf("cappedRetryAfter(empty) = %v, want 0", got)
	}
}

func TestRetryAfterHint(t *testing.T) {
	h := http.Header{"Retry-After": []string{"2"}}
	if got := RetryAfterHint(h); got != 2*time.Second {
		t.Errorf("RetryAfterHint = %v", got)
	}
	if got := RetryAfterHint(http.Header{}); got != 0 {
		t.Errorf("empty header = %v", got)
	}
	if got := RetryAfterHint(http.Header{"Retry-After": []string{"soon"}}); got != 0 {
		t.Errorf("garbage header = %v", got)
	}
}

func TestClientRequiresRequestedAnswers(t *testing.T) {
	for _, questions := range []map[string]Question{
		{"another_question": Noul("yes?")},
		{"is_urgent": Noul("yes?"), "missing": Noul("yes?")},
		{"is_urgent": Choice("pick?", map[string]string{"a": ""})},
	} {
		c := testClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, docsNoulResponse), nil
		}))
		if _, err := c.SystemOne(context.Background(), "state", questions); err == nil {
			t.Errorf("accepted wrong/incomplete response for %v", questions)
		}
	}
}

func TestValidateRejectsMalformedScoreAndChoice(t *testing.T) {
	for _, body := range []string{
		strings.Replace(docsScoreResponse, `"score":1.6`, `"score":3`, 1),
		strings.Replace(docsScoreResponse, `"score":1.6`, `"score":-1`, 1),
		strings.Replace(docsScoreResponse, `"0":0.05,"1":0.3,"2":0.65`, `"1":0.3,"2":0.7`, 1),
		strings.Replace(docsChoiceResponse, `"choice":"technical"`, `"choice":"billing"`, 1),
	} {
		if _, err := decodeResponse(t, body); err == nil {
			t.Errorf("accepted malformed answer: %s", body)
		}
	}
	resp, err := decodeResponse(t, docsScoreResponse)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1)} {
		ans := resp.Answers["frustration"]
		ans.Score = &value
		if err := ans.Validate(); err == nil {
			t.Errorf("accepted non-finite score %v", value)
		}
	}
	if err := resp.ValidateFor(map[string]Question{
		"frustration": Score("Rate frustration", []string{"Other", "Frustrated", "Very angry"}),
	}); err == nil {
		t.Error("accepted a different score rubric")
	}
}

func TestRetryAfterDateAndOverflow(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{now.Add(-time.Second).Format(http.TimeFormat), 0},
		{" 2 ", 2 * time.Second},
		{"-1", 0},
	} {
		if got := retryAfterAt(tc.value, now); got != tc.want {
			t.Errorf("Retry-After %q = %v, want %v", tc.value, got, tc.want)
		}
	}
	for _, value := range []string{"9223372036854775807", "18446744073709551616000"} {
		if got := cappedRetryAfter(http.Header{"Retry-After": {value}}); got != maxRetryAfter {
			t.Errorf("overflowing Retry-After %q = %v", value, got)
		}
	}
}

func TestMissingAPIKeyDoesNotCallTransport(t *testing.T) {
	c := NewClient(" ", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing key must not send a request")
		return nil, nil
	})}))
	_, err := c.SystemOne(context.Background(), "state", map[string]Question{"q": Noul("yes?")})
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestExtractMessageAndAPIErrorFormatting(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"message":"top-level message"}`, "top-level message"},
		{`{"detail":"validation detail"}`, "validation detail"},
		{`{"error":"string error"}`, "string error"},
		{`{"error":{"message":"nested message"}}`, "nested message"},
		{"  gateway upstream timeout \n", "gateway upstream timeout"},
	} {
		if got := extractMessage([]byte(tc.body)); got != tc.want {
			t.Errorf("extractMessage(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
	if got := (&APIError{StatusCode: 502}).Error(); got != "jev: API error 502" {
		t.Errorf("APIError without message = %q", got)
	}
	if got := (&APIError{StatusCode: 401, Message: "bad token"}).Error(); got != "jev: API error 401: bad token" {
		t.Errorf("APIError with message = %q", got)
	}
}

func TestValidateAnswersChoiceOptionsAndQuestionKeys(t *testing.T) {
	resp, err := decodeResponse(t, docsChoiceResponse)
	if err != nil {
		t.Fatal(err)
	}
	// Missing an expected option or wrong option count must fail ValidateFor.
	for _, opts := range []map[string]string{
		{"billing": "", "technical": ""},
		{"billing": "", "technical": "", "other": ""},
	} {
		if err := resp.ValidateFor(map[string]Question{"department": Choice("pick?", opts)}); err == nil {
			t.Errorf("accepted mismatched choice options %v", opts)
		}
	}
	scoreResp, err := decodeResponse(t, docsScoreResponse)
	if err != nil {
		t.Fatal(err)
	}
	if err := scoreResp.ValidateFor(map[string]Question{"frustration": Score("rate?", []string{"Calm", "Very angry"})}); err == nil {
		t.Error("accepted score answer with wrong number of levels")
	}

	multi := Response{Answers: map[string]Answer{"z": {}, "a": {}, "m": {}}}
	if got := multi.QuestionKeys(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Errorf("QuestionKeys() = %v, want [a m z]", got)
	}
}

func TestRetrySleepAbortsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		resp := jsonResponse(http.StatusTooManyRequests, `{"error":"rate limited"}`)
		resp.Header.Set("Retry-After", "30")
		go func() {
			time.Sleep(15 * time.Millisecond)
			cancel()
		}()
		return resp, nil
	})
	start := time.Now()
	_, err := testClient(rt).SystemOne(ctx, "s", map[string]Question{"q": Noul("yes?")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("canceled retry took %v, expected immediate abort", elapsed)
	}
}
