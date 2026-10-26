package domain

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
)

// DrawnQuestion is a drawn question with its options in displayed order.
type DrawnQuestion struct {
	Question Question
	Options  []Option
	Pace     float64
}

// SessionDraw is the questions of an attempt in presentation order.
type SessionDraw struct {
	Questions        []DrawnQuestion
	SuggestedMinutes int
}

// quotaGroup is the alive questions of one weighted primary category and how
// many a simulation takes from it.
type quotaGroup struct {
	quota     int
	questions []Question // sorted by id
}

// sessionPool is what a config selects from a bank: the pool, how many of it
// a session draws and the shortages that would stop the start.
type sessionPool struct {
	kind           SelectionKind
	questions      []Question // simulation: the weighted categories' questions; origin-set: in play order; else by id
	classification *Classification
	groups         []quotaGroup // simulation only
	target         int          // questions a session draws; for a simulation, the sum of the quotas
	shortages      []Shortage
}

func (p sessionPool) size() int { return len(p.questions) }

func sortedByID(qs []Question) []Question {
	out := slices.Clone(qs)
	slices.SortFunc(out, func(a, b Question) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func inCategory(q Question, class, category string) bool {
	return slices.ContainsFunc(q.Categories, func(r CategoryRef) bool {
		return Fold(r.Classification) == class && Fold(r.Category) == category
	})
}

func notFound(field, message string) *ValidationError {
	return SingleViolation(field, RuleNotFound, message)
}

// buildPool resolves a valid config against a bank (FR-7, FR-9).
func buildPool(exam Exam, bank Bank, cfg SessionConfig) (sessionPool, error) {
	pool := sessionPool{kind: cfg.Selection.Kind}
	primary := func() *Classification {
		if i := bank.primary(); i >= 0 {
			return &bank.Classifications[i]
		}
		return nil
	}
	emptyShortage := func(label string) { pool.shortages = []Shortage{{Category: label, Needed: 1, Available: 0}} }

	switch sel := cfg.Selection; sel.Kind {
	case SelectSimulation:
		pool.classification = primary()
		buildSimulationPool(&pool, exam, bank)
		return pool, nil

	case SelectRandom:
		pool.classification = primary()
		pool.questions = sortedByID(bank.Questions)
		pool.target = min(sel.Count, len(pool.questions))
		if pool.target == 0 {
			emptyShortage("bank")
		}

	case SelectCategories:
		var class *Classification
		for i := range bank.Classifications {
			if bank.Classifications[i].ID == sel.ClassificationID {
				class = &bank.Classifications[i]
			}
		}
		if class == nil {
			return pool, notFound("config.selection.classificationId", "the exam has no such classification")
		}
		pool.classification = class
		var names []string
		var wanted []string // folded
		for i, id := range sel.CategoryIDs {
			idx := slices.IndexFunc(class.Categories, func(c Category) bool { return c.ID == id })
			if idx < 0 {
				return pool, notFound(fmt.Sprintf("config.selection.categoryIds[%d]", i), "the classification has no such category")
			}
			names = append(names, class.Categories[idx].Name)
			wanted = append(wanted, Fold(class.Categories[idx].Name))
		}
		classFold := Fold(class.Name)
		for _, q := range sortedByID(bank.Questions) {
			if slices.ContainsFunc(wanted, func(w string) bool { return inCategory(q, classFold, w) }) {
				pool.questions = append(pool.questions, q)
			}
		}
		pool.target = len(pool.questions)
		if !sel.All {
			pool.target = min(sel.Count, len(pool.questions))
		}
		if pool.target == 0 {
			emptyShortage(strings.Join(names, ", "))
		}

	case SelectOriginSet:
		pool.classification = primary()
		for _, q := range bank.Questions {
			if q.Origin != nil && q.Origin.Set == sel.Set {
				pool.questions = append(pool.questions, q)
			}
		}
		// By origin.number, then key; a question with no number (0) comes after
		// the numbered ones, as in the bank browser.
		slices.SortFunc(pool.questions, func(a, b Question) int {
			if (a.Origin.Number > 0) != (b.Origin.Number > 0) {
				if a.Origin.Number > 0 {
					return -1
				}
				return 1
			}
			if a.Origin.Number != b.Origin.Number {
				return a.Origin.Number - b.Origin.Number
			}
			return strings.Compare(a.Key, b.Key)
		})
		pool.target = len(pool.questions)
		if pool.target == 0 {
			emptyShortage(sel.Set)
		}
	}
	return pool, nil
}

// withoutWeightLabel names the shortage of a simulation with nothing to draw from.
const withoutWeightLabel = "weighted categories"

// buildSimulationPool apportions the exam's question count over the weighted
// categories of the primary classification (FR-9, AC-14) and lists the
// categories that cannot fill their quota (AC-15).
func buildSimulationPool(pool *sessionPool, exam Exam, bank Bank) {
	var weighted []Category
	if pool.classification != nil {
		for _, c := range pool.classification.Categories {
			if c.WeightPercent > 0 {
				weighted = append(weighted, c)
			}
		}
	}
	if len(weighted) == 0 {
		pool.shortages = []Shortage{{Category: withoutWeightLabel, Needed: exam.QuestionCount, Available: 0}}
		return
	}
	shares := make([]int, len(weighted))
	for i, c := range weighted {
		shares[i] = c.WeightPercent
	}
	quotas := LargestRemainder(exam.QuestionCount, shares)

	class := Fold(pool.classification.Name)
	sorted := sortedByID(bank.Questions)
	for i, c := range weighted {
		group := quotaGroup{quota: quotas[i]}
		for _, q := range sorted {
			if inCategory(q, class, Fold(c.Name)) {
				group.questions = append(group.questions, q)
			}
		}
		pool.questions = append(pool.questions, group.questions...)
		pool.target += group.quota
		if group.quota > 0 {
			pool.groups = append(pool.groups, group)
			if len(group.questions) < group.quota {
				pool.shortages = append(pool.shortages, Shortage{Category: c.Name, Needed: group.quota, Available: len(group.questions)})
			}
		}
	}
}

// DrawSession selects, orders and prices the questions of a session with the
// seeded generator (FR-9). A *ValidationError with Shortage details when the
// bank cannot satisfy it. The config must already satisfy Violations.
//
// One PCG stream seeded with the seed alone drives everything, in this order:
// the per-category shuffles (simulation) or the pool shuffle (random,
// categories), the final question shuffle (simulation), then one option
// shuffle per drawn question. Pools start sorted by id, so the draw does not
// depend on the order the bank was read in.
func DrawSession(exam Exam, bank Bank, cfg SessionConfig, seed uint64) (SessionDraw, error) {
	pool, err := buildPool(exam, bank, cfg)
	if err != nil {
		return SessionDraw{}, err
	}
	if len(pool.shortages) > 0 {
		return SessionDraw{}, shortageViolations(pool.shortages)
	}
	rng := rand.New(rand.NewPCG(seed, seed))

	var chosen []Question
	switch pool.kind {
	case SelectSimulation:
		for _, g := range pool.groups {
			shuffled := slices.Clone(g.questions)
			rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			chosen = append(chosen, shuffled[:g.quota]...)
		}
		rng.Shuffle(len(chosen), func(i, j int) { chosen[i], chosen[j] = chosen[j], chosen[i] })
	case SelectOriginSet:
		chosen = pool.questions
	default:
		shuffled := slices.Clone(pool.questions)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		chosen = shuffled[:pool.target]
	}

	draw := SessionDraw{Questions: make([]DrawnQuestion, len(chosen))}
	total := 0.0
	for i, q := range chosen {
		options := slices.Clone(q.Options)
		rng.Shuffle(len(options), func(a, b int) { options[a], options[b] = options[b], options[a] })
		pace := QuestionPace(exam, pool.classification, q)
		total += pace
		draw.Questions[i] = DrawnQuestion{Question: q, Options: options, Pace: pace}
	}
	if pool.kind == SelectSimulation {
		draw.SuggestedMinutes = exam.TimeLimitMin
	} else {
		draw.SuggestedMinutes = CeilMinutes(total)
	}
	return draw, nil
}
