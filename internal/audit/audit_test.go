package audit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amscotti/nameworthy/internal/jev"
)

func TestQuestionsShape(t *testing.T) {
	q := Questions("", "")
	if len(q) != 10 {
		t.Fatalf("questions = %d, want 10 without race/class (fit is optional)", len(q))
	}
	if _, ok := q[KeyFit]; ok {
		t.Error("fit question must not exist without race/class context")
	}
	for _, d := range dims {
		got, ok := q[d.key]
		if !ok {
			t.Errorf("missing dimension question %q", d.key)
			continue
		}
		if got.Type != jev.TypeScore {
			t.Errorf("%q type = %s, want score", d.key, got.Type)
		}
	}

	withFit := Questions("Orc", "Warrior")
	if len(withFit) != 11 {
		t.Fatalf("questions with context = %d, want 11", len(withFit))
	}
	fit, ok := withFit[KeyFit]
	if !ok || fit.Type != jev.TypeNoul {
		t.Fatalf("fit question = %+v, want a noul", fit)
	}

	if q[KeyGrade].Type != jev.TypeChoice || q[KeyArchetype].Type != jev.TypeChoice {
		t.Error("grade and archetype must be choice questions")
	}
	if q[KeyVulgar].Type != jev.TypeNoul || q[KeyReportable].Type != jev.TypeNoul {
		t.Error("vulgar and reportable must be noul questions")
	}

	// Race-only and class-only contexts must each ask a clean fit question.
	if got := Questions("Night Elf", "")[KeyFit].Instructions; !strings.HasSuffix(got, ": Night Elf?") {
		t.Errorf("race-only fit prompt = %q", got)
	}
	if got := Questions("", "Druid")[KeyFit].Instructions; !strings.HasSuffix(got, ": Druid?") {
		t.Errorf("class-only fit prompt = %q", got)
	}
}

// scoreAnswer builds a real-shaped score answer: probabilities keyed by level
// number ("0".."n") and score a raw position on that number line.
func scoreAnswer(t *testing.T, raw float64, probs map[string]float64) jev.Answer {
	t.Helper()
	conf := 0.5
	return jev.Answer{Type: jev.TypeScore, Score: &raw, Probabilities: probs, Confidence: &conf}
}

func noulAnswer(t *testing.T, p float64) jev.Answer {
	t.Helper()
	return jev.Answer{Type: jev.TypeNoul, Noul: &p}
}

func choiceAnswer(t *testing.T, choice string, probs map[string]float64) jev.Answer {
	t.Helper()
	conf := 0.7
	return jev.Answer{Type: jev.TypeChoice, Choice: &choice, Probabilities: probs, Confidence: &conf}
}

func fullAnswers(t *testing.T) map[string]jev.Answer {
	t.Helper()
	answers := map[string]jev.Answer{
		KeyLoreFit:    scoreAnswer(t, 3.6, map[string]float64{"2": 0.1, "3": 0.2, "4": 0.7}),
		KeyHumor:      scoreAnswer(t, 1.2, map[string]float64{"0": 0.1, "1": 0.6, "2": 0.3}),
		KeyCreativity: scoreAnswer(t, 2.4, map[string]float64{"2": 0.6, "3": 0.4}),
		KeyPronounce:  scoreAnswer(t, 3.2, map[string]float64{"2": 0.05, "3": 0.7, "4": 0.25}),
		KeyImmersion:  scoreAnswer(t, 3.4, map[string]float64{"2": 0.1, "3": 0.4, "4": 0.5}),
		KeyEffort:     scoreAnswer(t, 2.8, map[string]float64{"0": 0.2, "3": 0.4, "4": 0.4}),
		KeyVulgar:     noulAnswer(t, 0.05),
		KeyReportable: noulAnswer(t, 0.1),
		KeyFit:        noulAnswer(t, 0.8),
		KeyGrade: choiceAnswer(t, "S", map[string]float64{
			"S": 0.35, "A": 0.35, "B": 0.2, "C": 0.1,
		}),
		KeyArchetype: choiceAnswer(t, "serious_rp", map[string]float64{
			"serious_rp": 0.5, "lore_master": 0.3, "gamer_tag": 0.1, "generic": 0.1,
		}),
	}
	for _, d := range dims {
		ans := answers[d.key]
		ans.Legend = map[string]string{}
		for i, level := range d.levels {
			key := fmt.Sprint(i)
			ans.Legend[key] = level
			if _, ok := ans.Probabilities[key]; !ok {
				ans.Probabilities[key] = 0
			}
		}
		answers[d.key] = ans
	}
	for _, g := range gradeOptions {
		if _, ok := answers[KeyGrade].Probabilities[g.id]; !ok {
			answers[KeyGrade].Probabilities[g.id] = 0
		}
	}
	for _, a := range archetypeOptions {
		if _, ok := answers[KeyArchetype].Probabilities[a.id]; !ok {
			answers[KeyArchetype].Probabilities[a.id] = 0
		}
	}
	return answers
}

func TestFromAnswersHappyPath(t *testing.T) {
	a, err := FromAnswers(fullAnswers(t))
	if err != nil {
		t.Fatalf("FromAnswers: %v", err)
	}

	if len(a.Dims) != 6 {
		t.Fatalf("dims = %d, want the 6 ranked dimensions", len(a.Dims))
	}
	if a.Dims[0].Key != KeyLoreFit || a.Dims[5].Key != KeyEffort {
		t.Errorf("dims out of display order: %s…%s", a.Dims[0].Key, a.Dims[5].Key)
	}
	if a.Dims[0].Level != "textbook" || a.Dims[0].Score != 0.9 || a.Dims[0].Position != 3.6 {
		t.Errorf("lore_fit = %+v, want textbook at normalized 0.9 (raw 3.6)", a.Dims[0])
	}

	if len(a.Flags) != 3 {
		t.Fatalf("flags = %d, want 3 (vulgar, reportable, fit)", len(a.Flags))
	}
	if a.Flags[0].Key != KeyVulgar || a.Flags[0].Yes != 0.05 || a.Flags[2].Key != KeyFit {
		t.Errorf("flags wrong: %+v", a.Flags)
	}

	if a.Grade.ID != "S" { // tie 0.35/0.35 → Jev's returned choice is authoritative
		t.Errorf("grade = %+v, want S on the tie-break", a.Grade)
	}
	if a.Archetype.ID != "serious_rp" || a.Archetype.Label != "Serious RP" {
		t.Errorf("archetype = %+v, want Serious RP", a.Archetype)
	}
	if len(a.GradeDist) != 6 || len(a.ArchDist) != 8 {
		t.Errorf("distributions = %d grade / %d archetype, want 6 / 8", len(a.GradeDist), len(a.ArchDist))
	}
	if a.GradeDist[0].Probability < a.GradeDist[1].Probability {
		t.Error("grade distribution not sorted by probability")
	}
	// Eight answers carry confidence: six dims at 0.5 and both choices at 0.7.
	if a.Confidence != 0.55 {
		t.Errorf("confidence = %v, want the mean of the answered confidences (0.55)", a.Confidence)
	}
}

func TestFromAnswersWithoutFit(t *testing.T) {
	answers := fullAnswers(t)
	delete(answers, KeyFit)
	a, err := FromAnswers(answers)
	if err != nil {
		t.Fatalf("FromAnswers without fit: %v", err)
	}
	if len(a.Flags) != 2 {
		t.Errorf("flags = %d, want 2 when fit is absent", len(a.Flags))
	}
}

func TestFromAnswersErrors(t *testing.T) {
	cases := map[string]func(map[string]jev.Answer){
		"missing dim":    func(m map[string]jev.Answer) { delete(m, KeyHumor) },
		"wrong dim type": func(m map[string]jev.Answer) { m[KeyHumor] = noulAnswer(t, 0.5) },
		"missing vulgar": func(m map[string]jev.Answer) { delete(m, KeyVulgar) },
		"bad noul":       func(m map[string]jev.Answer) { m[KeyVulgar] = scoreAnswer(t, 0.5, map[string]float64{"0": 1}) },
		"bad grade type": func(m map[string]jev.Answer) { m[KeyGrade] = noulAnswer(t, 0.5) },
		"unknown option": func(m map[string]jev.Answer) { m[KeyGrade] = choiceAnswer(t, "Z", map[string]float64{"Z": 1}) },
		"empty grade":    func(m map[string]jev.Answer) { m[KeyGrade] = choiceAnswer(t, "S", nil) },
		"missing arch":   func(m map[string]jev.Answer) { delete(m, KeyArchetype) },
		"missing level":  func(m map[string]jev.Answer) { delete(m[KeyHumor].Probabilities, "4") },
		"bad flag range": func(m map[string]jev.Answer) { m[KeyVulgar] = noulAnswer(t, 2) },
		"out of span": func(m map[string]jev.Answer) {
			v := 5.0
			ans := m[KeyHumor]
			ans.Score = &v
			m[KeyHumor] = ans
		},
	}
	for name, mutate := range cases {
		answers := fullAnswers(t)
		mutate(answers)
		if _, err := FromAnswers(answers); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestFromAnswersHonorsReturnedChoiceOnTie(t *testing.T) {
	answers := fullAnswers(t)
	ans := answers[KeyGrade]
	choice := "A"
	ans.Choice = &choice
	// B and C also tie below the winner; display order (B before C) settles secondary ties.
	ans.Probabilities = map[string]float64{"S": 0.35, "A": 0.35, "B": 0.15, "C": 0.15, "D": 0, "F": 0}
	answers[KeyGrade] = ans
	got, err := FromAnswers(answers)
	if err != nil || got.Grade.ID != "A" || got.GradeDist[0].ID != "A" {
		t.Fatalf("tied verdict = %+v, err = %v; want Jev's A", got.Grade, err)
	}
	if got.GradeDist[2].ID != "B" || got.GradeDist[3].ID != "C" {
		t.Errorf("secondary tie order = %v, want B then C", got.GradeDist)
	}
	if normalize(-0.5, 5) != 0 || normalize(4.5, 5) != 1 || normalize(1, 1) != 0 {
		t.Error("normalize must clamp out-of-range positions and degenerate spans")
	}
}
