package jev

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Primitives: every question is one of these three shapes.
const (
	TypeNoul   = "noul"
	TypeChoice = "choice"
	TypeScore  = "score"
)

// NoulCriteria pins down what a yes and a no mean when the boundary is subtle.
type NoulCriteria struct {
	True  string `json:"true"`
	False string `json:"false"`
}

// Question is a typed question evaluated against the state. Criteria carries
// the per-type rubric and matches one of the documented shapes:
//
//	noul   -> nil or NoulCriteria
//	choice -> map[string]*string (option -> description, nil = no detail)
//	score  -> []string (ordered level descriptions, at least two)
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Noul builds a yes/no question.
func Noul(instructions string) Question {
	return Question{Type: TypeNoul, Instructions: instructions}
}

// NoulWithCriteria builds a yes/no question with explicit yes/no meanings.
func NoulWithCriteria(instructions, yesMeaning, noMeaning string) Question {
	return Question{
		Type:         TypeNoul,
		Instructions: instructions,
		Criteria:     NoulCriteria{True: yesMeaning, False: noMeaning},
	}
}

// Choice builds a question that picks one option from options (option ->
// rubric description; an empty description needs no extra detail).
func Choice(instructions string, options map[string]string) Question {
	criteria := make(map[string]*string, len(options))
	for opt, desc := range options {
		if desc == "" {
			criteria[opt] = nil
			continue
		}
		d := desc
		criteria[opt] = &d
	}
	return Question{Type: TypeChoice, Instructions: instructions, Criteria: criteria}
}

// Score builds a question that rates the state along ordered levels.
func Score(instructions string, levels []string) Question {
	return Question{Type: TypeScore, Instructions: instructions, Criteria: slices.Clone(levels)}
}

// Validate checks the question against the documented request schema.
func (q Question) Validate() error {
	if strings.TrimSpace(q.Instructions) == "" {
		return fmt.Errorf("jev: question %q: instructions must not be empty", q.Type)
	}
	switch q.Type {
	case TypeNoul:
		switch q.Criteria.(type) {
		case nil, NoulCriteria:
			return nil
		default:
			return fmt.Errorf("jev: noul criteria must be a {true,false} object")
		}
	case TypeChoice:
		crit, ok := q.Criteria.(map[string]*string)
		if !ok || len(crit) == 0 || len(crit) > 255 {
			return fmt.Errorf("jev: choice criteria must contain 1 to 255 options")
		}
		return nil
	case TypeScore:
		levels, ok := q.Criteria.([]string)
		if !ok || len(levels) < 2 || len(levels) > 10 {
			return fmt.Errorf("jev: score criteria must contain 2 to 10 ordered levels")
		}
		return nil
	default:
		return fmt.Errorf("jev: unknown question type %q", q.Type)
	}
}

// Usage reports the tokens the request billed. Output tokens are free on Jev.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Answer is one typed answer, tagged by Type with the fields belonging to
// that type populated. Noul answers carry only Noul; choice answers carry
// Choice, Probabilities and Confidence; score answers carry Score, Legend,
// Probabilities and Confidence.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        *string            `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
}

type rawAnswer Answer // no UnmarshalJSON method; avoids recursive decoding

// UnmarshalJSON decodes the tagged answer union and validates it.
func (a *Answer) UnmarshalJSON(data []byte) error {
	var raw rawAnswer
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("jev: decoding answer: %w", err)
	}
	*a = Answer(raw)
	return a.Validate()
}

// Validate checks the answer against the documented response schema:
// noul in [0,1]; choice and score answers carry probabilities that sum to
// one and a confidence in [0,1]; score answers carry their legend.
func (a Answer) Validate() error {
	switch a.Type {
	case TypeNoul:
		if a.Noul == nil {
			return fmt.Errorf("jev: noul answer missing \"noul\"")
		}
		if err := unitInterval("noul", *a.Noul); err != nil {
			return err
		}
		return nil
	case TypeChoice:
		if a.Choice == nil {
			return fmt.Errorf("jev: choice answer missing \"choice\"")
		}
		if _, ok := a.Probabilities[*a.Choice]; !ok {
			return fmt.Errorf("jev: choice %q is not among the probabilities", *a.Choice)
		}
		for _, p := range a.Probabilities {
			if p > a.Probabilities[*a.Choice] {
				return fmt.Errorf("jev: choice %q is not a highest-probability option", *a.Choice)
			}
		}
		return a.validateDistribution("choice")
	case TypeScore:
		if a.Score == nil {
			return fmt.Errorf("jev: score answer missing \"score\"")
		}
		if len(a.Legend) < 2 {
			return fmt.Errorf("jev: score answer needs an ordered legend of at least two levels")
		}
		if math.IsNaN(*a.Score) || math.IsInf(*a.Score, 0) || *a.Score < 0 || *a.Score > float64(len(a.Legend)-1) {
			return fmt.Errorf("jev: score is outside its legend's level span")
		}
		if len(a.Legend) != len(a.Probabilities) {
			return fmt.Errorf("jev: score probabilities must cover every legend level")
		}
		for i := range len(a.Legend) {
			level := strconv.Itoa(i)
			if _, ok := a.Legend[level]; !ok {
				return fmt.Errorf("jev: score legend missing level %q", level)
			}
			if _, ok := a.Probabilities[level]; !ok {
				return fmt.Errorf("jev: score probabilities missing level %q", level)
			}
		}
		return a.validateDistribution("score")
	default:
		return fmt.Errorf("jev: unknown answer type %q", a.Type)
	}
}

func (a Answer) validateDistribution(kind string) error {
	if len(a.Probabilities) == 0 {
		return fmt.Errorf("jev: %s answer missing probabilities", kind)
	}
	sum := 0.0
	for key, p := range a.Probabilities {
		if err := unitInterval(kind+" probability "+key, p); err != nil {
			return err
		}
		sum += p
	}
	if math.Abs(sum-1) > 0.02 {
		return fmt.Errorf("jev: %s probabilities sum to %.3f, want 1", kind, sum)
	}
	if a.Confidence == nil {
		return fmt.Errorf("jev: %s answer missing \"confidence\"", kind)
	}
	return unitInterval("confidence", *a.Confidence)
}

func unitInterval(name string, v float64) error {
	if math.IsNaN(v) || v < 0 || v > 1 {
		return fmt.Errorf("jev: %s = %v, want a value in [0,1]", name, v)
	}
	return nil
}

// Response is one System One evaluation: one answer per question, keyed by
// the ids the caller chose.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// UnmarshalJSON preserves the distinction between missing usage and a valid
// zero-token envelope. Missing provenance must not look like a free audit.
func (r *Response) UnmarshalJSON(data []byte) error {
	var raw struct {
		Model   string            `json:"model"`
		Answers map[string]Answer `json:"answers"`
		Usage   *Usage            `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("jev: decoding response: %w", err)
	}
	if raw.Usage == nil {
		return fmt.Errorf("jev: response missing \"usage\"")
	}
	*r = Response{Model: raw.Model, Answers: raw.Answers, Usage: *raw.Usage}
	return r.Validate()
}

// Validate also checks programmatically built answers, not just decoded ones.
func (r Response) Validate() error {
	if r.Model == "" {
		return fmt.Errorf("jev: response missing \"model\"")
	}
	if len(r.Answers) == 0 {
		return fmt.Errorf("jev: response missing \"answers\"")
	}
	for key, ans := range r.Answers {
		if err := ans.Validate(); err != nil {
			return fmt.Errorf("jev: answer %q: %w", key, err)
		}
	}
	if r.Usage.InputTokens < 0 || r.Usage.OutputTokens < 0 {
		return fmt.Errorf("jev: negative token usage")
	}
	return nil
}

// ValidateFor insists that the response answers the exact requested questions.
func (r Response) ValidateFor(questions map[string]Question) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return ValidateAnswers(r.Answers, questions)
}

// ValidateAnswers checks question coverage and the request-specific rubrics.
// A valid-looking distribution for the wrong question is still a bad answer.
func ValidateAnswers(answers map[string]Answer, questions map[string]Question) error {
	if len(answers) != len(questions) {
		return fmt.Errorf("jev: got %d answers for %d questions", len(answers), len(questions))
	}
	for key, q := range questions {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("jev: question %q: %w", key, err)
		}
		ans, ok := answers[key]
		if !ok {
			return fmt.Errorf("jev: missing answer %q", key)
		}
		if err := ans.Validate(); err != nil {
			return fmt.Errorf("jev: answer %q: %w", key, err)
		}
		if ans.Type != q.Type {
			return fmt.Errorf("jev: answer %q type = %q, want %q", key, ans.Type, q.Type)
		}
		switch q.Type {
		case TypeChoice:
			options := q.Criteria.(map[string]*string)
			if len(ans.Probabilities) != len(options) {
				return fmt.Errorf("jev: answer %q must cover every choice option", key)
			}
			for id := range options {
				if _, ok := ans.Probabilities[id]; !ok {
					return fmt.Errorf("jev: answer %q missing option %q", key, id)
				}
			}
		case TypeScore:
			levels := q.Criteria.([]string)
			if len(ans.Legend) != len(levels) {
				return fmt.Errorf("jev: answer %q has the wrong number of score levels", key)
			}
			for i, description := range levels {
				if ans.Legend[strconv.Itoa(i)] != description {
					return fmt.Errorf("jev: answer %q has a different rubric at level %d", key, i)
				}
			}
		}
	}
	return nil
}

// QuestionKeys returns the answered question ids in sorted order; callers use
// it to iterate answers deterministically.
func (r Response) QuestionKeys() []string {
	keys := make([]string, 0, len(r.Answers))
	for k := range r.Answers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// APIError is a non-2xx response from the TypeSafe API. The error body shape
// is not fully specified, so Message is extracted best-effort from the common
// JSON envelopes and Raw keeps the body for diagnostics.
type APIError struct {
	StatusCode int
	Message    string
	Raw        string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("jev: API error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("jev: API error %d", e.StatusCode)
}
