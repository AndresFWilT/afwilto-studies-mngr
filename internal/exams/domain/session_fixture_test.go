package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Fixtures of the slice 06a tests. Question ids are "q-<key>"; the options of a
// question read "<key>/a".."<key>/d" with "<key>/b" the only correct one, so a
// test can tell which option is which after the shuffle.

func sessExam() Exam {
	return Exam{ID: "exam-1", Name: RehydrateExamName("SAA-C03"), QuestionCount: 65, TimeLimitMin: 130, PassPercent: 72}
}

func sessQ(key string, refs ...CategoryRef) Question {
	q := Question{ID: "q-" + key, ExamID: "exam-1", Key: key, Type: QuestionSingle, Stem: "stem " + key,
		Explanation: "explanation " + key, Version: 1, Categories: refs}
	for i, letter := range []string{"a", "b", "c", "d"} {
		q.Options = append(q.Options, Option{Text: key + "/" + letter, Correct: i == 1, Rationale: "why " + key + "/" + letter})
	}
	return q
}

func withOrigin(q Question, set string, number int) Question {
	q.Origin = &Origin{Set: set, Number: number, NoteRef: "note of " + q.Key}
	return q
}

func manyQ(prefix string, n int, refs ...CategoryRef) []Question {
	out := make([]Question, n)
	for i := range out {
		out[i] = sessQ(fmt.Sprintf("%s-%02d", prefix, i), refs...)
	}
	return out
}

func dom(cat string) CategoryRef { return ref2("Dominio", cat) }
func svc(cat string) CategoryRef { return ref2("Servicio", cat) }

// awsBank is the AC-14 fixture: "Dominio" is primary with manual weights
// 30/26/24/20 and 30 questions each, "Legacy" has weight 0 and 10 questions,
// 10 questions have no Dominio at all (only a Servicio).
func awsBank() Bank {
	dominio := bkClass("cl-dom", "Dominio", PositionPrimary, WeightManual,
		bkCat("c-seg", "Seguridad", 30), bkCat("c-res", "Resiliente", 26),
		bkCat("c-per", "Rendimiento", 24), bkCat("c-cos", "Costos", 20), bkCat("c-leg", "Legacy", 0))
	servicio := bkClass("cl-svc", "Servicio", PositionSecondary, WeightInferred, bkCat("c-vpc", "VPC", 0))
	var qs []Question
	qs = append(qs, manyQ("seg", 30, dom("Seguridad"))...)
	qs = append(qs, manyQ("res", 30, dom("Resiliente"))...)
	qs = append(qs, manyQ("per", 30, dom("Rendimiento"))...)
	qs = append(qs, manyQ("cos", 30, dom("Costos"))...)
	qs = append(qs, manyQ("leg", 10, dom("Legacy"))...)
	qs = append(qs, manyQ("unc", 10, svc("VPC"))...)
	return Bank{Classifications: []Classification{dominio, servicio}, Questions: qs}
}

func simulation() SessionConfig { return SessionConfig{Selection: Selection{Kind: SelectSimulation}} }

func practice(sel Selection) SessionConfig {
	return SessionConfig{Selection: sel, Timing: &Timing{Kind: Untimed}, Feedback: FeedbackEnd}
}

// byDomain counts a draw per Dominio category name ("" for no Dominio).
func byDomain(draw SessionDraw) map[string]int {
	counts := map[string]int{}
	for _, d := range draw.Questions {
		name := ""
		for _, ref := range d.Question.Categories {
			if ref.Classification == "Dominio" {
				name = ref.Category
			}
		}
		counts[name]++
	}
	return counts
}

func drawnKeys(draw SessionDraw) string {
	keys := make([]string, len(draw.Questions))
	for i, d := range draw.Questions {
		keys[i] = d.Question.Key
	}
	return strings.Join(keys, ",")
}

func optionTexts(draw SessionDraw) string {
	var parts []string
	for _, d := range draw.Questions {
		for _, o := range d.Options {
			parts = append(parts, o.Text)
		}
	}
	return strings.Join(parts, ",")
}

func shortagesOf(err error) []Shortage {
	var v *ValidationError
	if !errors.As(err, &v) {
		return nil
	}
	var out []Shortage
	for _, viol := range v.Violations {
		if viol.Rule == RuleInsufficientQuestions && viol.Shortage != nil {
			out = append(out, *viol.Shortage)
		}
	}
	return out
}
