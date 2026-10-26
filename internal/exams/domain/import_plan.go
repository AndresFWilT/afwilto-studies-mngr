package domain

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// catUse and classUse are the categories of one file question after folding:
// classifications and categories that differ only in case, accents or spacing
// are one, and the first spelling wins.
type catUse struct{ fold, name string }

type classUse struct {
	fold, name string
	cats       []catUse
}

func questionClasses(q FileQuestion) []classUse {
	var out []classUse
	for _, cn := range q.Categories {
		fold := Fold(cn.Classification)
		idx := -1
		for i := range out {
			if out[i].fold == fold {
				idx = i
			}
		}
		if idx < 0 {
			out = append(out, classUse{fold: fold, name: strings.TrimSpace(cn.Classification)})
			idx = len(out) - 1
		}
		for _, n := range cn.Names {
			cf := Fold(n)
			if !containsCat(out[idx].cats, cf) {
				out[idx].cats = append(out[idx].cats, catUse{fold: cf, name: strings.Join(strings.Fields(n), " ")})
			}
		}
	}
	return out
}

func containsCat(cats []catUse, fold string) bool {
	for _, c := range cats {
		if c.fold == fold {
			return true
		}
	}
	return false
}

func findUse(uses []classUse, fold string) *classUse {
	for i := range uses {
		if uses[i].fold == fold {
			return &uses[i]
		}
	}
	return nil
}

// regCat and regClass form the registry of classifications and categories the
// import works against: the exam's existing ones first, then the new ones in
// order of first appearance.
type regCat struct {
	id, key, name, fold string
	existing            bool
}

type regClass struct {
	id, name, fold string
	existing       bool
	position       int
	mode           WeightMode
	cats           []*regCat
}

func findClass(reg []*regClass, fold string) *regClass {
	for _, c := range reg {
		if c.fold == fold {
			return c
		}
	}
	return nil
}

func findCat(c *regClass, fold string) *regCat {
	for _, k := range c.cats {
		if k.fold == fold {
			return k
		}
	}
	return nil
}

func qPath(i int, field string) string {
	if field == "" {
		return fmt.Sprintf("questions[%d]", i)
	}
	if strings.HasPrefix(field, "[") {
		return fmt.Sprintf("questions[%d]%s", i, field)
	}
	return fmt.Sprintf("questions[%d].%s", i, field)
}

// violationPath turns a ValidateQuestion field ("options[1]", "key") into a path.
func violationPath(i int, field string) string { return qPath(i, field) }

var questionIndex = regexp.MustCompile(`^questions\[(\d+)\]`)

func questionIndexOf(path string) (int, bool) {
	m := questionIndex.FindStringSubmatch(path)
	if m == nil {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

// PlanFailure is the plan of an import that failed before PlanImport ran (the
// parse and JSON Schema stages): the errors, and no write.
func PlanFailure(errs []ImportError) ImportPlan { return failure(slices.Clone(errs), MediaReport{}) }

// failedPlan builds the plan of a pipeline that stopped at a failing stage.
func failedPlan(errs []ImportError, media MediaReport) ImportPlan {
	sortByQuestion(errs)
	return failure(errs, media)
}

// sortByQuestion lists question errors in file order, then the others.
func sortByQuestion(errs []ImportError) {
	sort.SliceStable(errs, func(a, b int) bool {
		ia, oka := questionIndexOf(errs[a].Path)
		ib, okb := questionIndexOf(errs[b].Path)
		switch {
		case oka && okb:
			return ia < ib
		case oka != okb:
			return oka
		}
		return false
	})
}

// MaxReportErrors bounds the errors one report carries, so a file that is
// wrong everywhere cannot produce a response of millions of entries.
const MaxReportErrors = 200

func failure(errs []ImportError, media MediaReport) ImportPlan {
	invalid := map[int]bool{}
	for _, e := range errs {
		if i, ok := questionIndexOf(e.Path); ok {
			invalid[i] = true
		}
	}
	report := ImportReport{Counts: ImportCounts{Invalid: len(invalid)}, Media: media}
	if len(errs) > MaxReportErrors {
		report.Warnings = []string{fmt.Sprintf("%d more errors not shown", len(errs)-MaxReportErrors)}
		errs = errs[:MaxReportErrors]
	}
	report.Errors = errs
	return ImportPlan{Report: report}
}

// PlanImport runs the semantic stages (3: BR-Q1/Q2/Q3, BR-C1, BR-C2; 4: media)
// and, when they pass, the inference and the diff against the bank. It does no
// I/O and does not modify its input. Each stage collects all its errors and
// the first failing stage ends the plan, with no Write.
func PlanImport(in PlanInput) ImportPlan {
	qs := in.File.Questions
	uses := make([][]classUse, len(qs))
	for i := range qs {
		uses[i] = questionClasses(qs[i])
	}

	// ── Stage 3: semantics ──────────────────────────────────────────────
	var errs []ImportError
	if src := in.File.Source; src != nil {
		for field, value := range map[string]string{"source.kind": src.Kind, "source.name": src.Name} {
			if hasNUL(value) {
				errs = append(errs, ImportError{Path: field, Rule: RuleInvalidText, Message: invalidText(field).Message})
			}
		}
		sort.Slice(errs, func(a, b int) bool { return errs[a].Path < errs[b].Path })
	}
	seenKeys := map[string]bool{}
	for i, q := range qs {
		for _, v := range ValidateQuestion(q) {
			errs = append(errs, ImportError{Path: violationPath(i, v.Field), Rule: v.Rule, Message: v.Message})
		}
		if q.Key != "" {
			if seenKeys[q.Key] {
				errs = append(errs, ImportError{Path: qPath(i, "key"), Rule: RuleDuplicateKey,
					Message: fmt.Sprintf("key %q appears more than once in the file", q.Key)})
			}
			seenKeys[q.Key] = true
		}
	}

	// BR-C1: classifications, existing first.
	var reg []*regClass
	existingPrimary := ""
	for _, c := range in.Bank.Classifications {
		rc := &regClass{id: c.ID, name: c.Name, fold: Fold(c.Name), existing: true, position: c.Position, mode: c.WeightMode}
		if rc.mode == "" {
			rc.mode = WeightInferred
		}
		for _, k := range c.Categories {
			rc.cats = append(rc.cats, &regCat{id: k.ID, key: k.Key, name: k.Name, fold: Fold(k.Name), existing: true})
		}
		reg = append(reg, rc)
		if c.IsPrimary() {
			existingPrimary = c.Name
		}
	}
	rejected := map[string]bool{}
	for i := range qs {
		kept := uses[i][:0:0]
		for _, cu := range uses[i] {
			if findClass(reg, cu.fold) == nil && !rejected[cu.fold] {
				if len(reg) >= MaxClassifications {
					rejected[cu.fold] = true
					errs = append(errs, ImportError{Path: qPath(i, "categories"), Rule: RuleTooManyClassifications,
						Message: fmt.Sprintf("classification %q would be a third one; an exam has at most %d", cu.name, MaxClassifications)})
				} else {
					reg = append(reg, &regClass{name: cu.name, fold: cu.fold, position: PositionSecondary, mode: WeightInferred})
				}
			}
			if !rejected[cu.fold] {
				kept = append(kept, cu)
			}
		}
		uses[i] = kept
	}

	// Existing questions the file does not rewrite still count for BR-C2.
	fileKeys := map[string]bool{}
	for _, q := range qs {
		fileKeys[q.Key] = true
	}
	type multi struct {
		count    int
		firstKey string
	}
	bankMulti := map[string]*multi{}
	for _, bq := range in.Bank.Questions {
		if fileKeys[bq.Key] {
			continue
		}
		perClass := map[string]map[string]bool{}
		for _, ref := range bq.Categories {
			cf := Fold(ref.Classification)
			if perClass[cf] == nil {
				perClass[cf] = map[string]bool{}
			}
			perClass[cf][Fold(ref.Category)] = true
		}
		for cf, set := range perClass {
			if len(set) > 1 {
				if bankMulti[cf] == nil {
					bankMulti[cf] = &multi{firstKey: bq.Key}
				}
				bankMulti[cf].count++
			}
		}
	}

	usages := make([]ClassificationUsage, len(reg))
	for ri, rc := range reg {
		// FR-3.1: no question lists more than one value in it. A question
		// without a value is just unclassified (FR-3.3); the file must use
		// the classification at least once.
		eligible := bankMulti[rc.fold] == nil
		used := false
		for i := range qs {
			cu := findUse(uses[i], rc.fold)
			if cu == nil {
				continue
			}
			used = true
			if len(cu.cats) > 1 {
				eligible = false
				break
			}
		}
		eligible = eligible && used
		usages[ri] = ClassificationUsage{Name: rc.name, Eligible: eligible}
	}
	if existingPrimary == "" && in.Primary != "" && findClass(reg, Fold(in.Primary)) == nil {
		errs = append(errs, ImportError{Path: "primary", Rule: RuleNotFound,
			Message: fmt.Sprintf("the primary classification %q is neither in the exam nor in the file", in.Primary)})
	}
	primaryName, hasPrimary := ResolvePrimary(existingPrimary, in.Primary, usages)
	primaryFold := Fold(primaryName)

	// BR-C2.
	if hasPrimary {
		for i := range qs {
			if cu := findUse(uses[i], primaryFold); cu != nil && len(cu.cats) > 1 {
				errs = append(errs, ImportError{Path: qPath(i, "categories"), Rule: RulePrimarySingleValued,
					Message: fmt.Sprintf("%q is the primary classification and takes one category per question; this question lists %d", primaryName, len(cu.cats))})
			}
		}
		if m := bankMulti[primaryFold]; m != nil && existingPrimary == "" {
			errs = append(errs, ImportError{Path: "primary", Rule: RulePrimarySingleValued,
				Message: fmt.Sprintf("%q cannot be primary: %d existing questions list more than one of its categories (first: %s)", primaryName, m.count, m.firstKey)})
		}
	}
	if len(errs) > 0 {
		return failedPlan(errs, MediaReport{})
	}

	// ── Stage 4: media ──────────────────────────────────────────────────
	media := resolveMedia(qs, in.Archive, in.KnownMedia)
	if len(media.errs) > 0 {
		return failedPlan(media.errs, media.report)
	}
	in.File.Questions = RewriteFileMedia(qs, media.rewrite)

	plan := buildPlan(in, uses, reg, primaryName, hasPrimary)
	plan.Report.Media = media.report
	plan.Write.Media = media.uploads
	return plan
}

// mediaResolution is the outcome of stage 4 (BR-Q4): the errors, the media
// part of the report, the images to store and the rewrite of the references.
type mediaResolution struct {
	errs    []ImportError
	report  MediaReport
	uploads []MediaFile
	hashes  map[string]string // zip path -> sha256 of its image
}

// rewrite maps a zip path to its hash; hash references and unknown targets stay.
func (m mediaResolution) rewrite(ref string) (string, bool) {
	sha, ok := m.hashes[ref]
	return sha, ok
}

// resolveMedia checks every media: reference. A hash must already be stored; a
// path must name a usable image of the zip. A missing target is reported at
// every place that uses it; an image with a problem once, at its first use.
func resolveMedia(qs []FileQuestion, archive *MediaArchive, known map[string]bool) mediaResolution {
	res := mediaResolution{hashes: map[string]string{}}
	names := map[string]bool{}
	if archive != nil {
		res.report.InZip = len(archive.Names)
		for _, n := range archive.Names {
			names[n] = true
		}
	}
	seen, reported, uploaded := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, r := range MediaRefs(qs) {
		first := !seen[r.Ref]
		seen[r.Ref] = true
		if first {
			res.report.Referenced++
		}
		missing := func(why string) {
			res.errs = append(res.errs, ImportError{Path: r.Path, Rule: RuleMediaMissing, Message: fmt.Sprintf("media:%s %s", r.Ref, why)})
			if first {
				res.report.Missing = append(res.report.Missing, r.Ref)
			}
		}
		switch {
		case IsMediaSHA(r.Ref):
			if !known[r.Ref] {
				missing("is not a stored image: include the image in a .zip")
			}
		case names[r.Ref]:
			file, ok := archive.Files[r.Ref]
			switch {
			case !ok:
				missing("is in the zip but was not read")
			case file.Problem != nil:
				if !reported[r.Ref] {
					reported[r.Ref] = true
					res.errs = append(res.errs, ImportError{Path: r.Path, Rule: file.Problem.Rule, Message: file.Problem.Message})
				}
			default:
				res.hashes[r.Ref] = file.Media.SHA256
				if !uploaded[file.Media.SHA256] {
					uploaded[file.Media.SHA256] = true
					res.uploads = append(res.uploads, file)
				}
			}
		case archive != nil:
			missing("is not in the zip")
		default:
			missing("needs a .zip that contains the image; this import is a .json file")
		}
	}
	for n := range names {
		if !seen[n] {
			res.report.Unused = append(res.report.Unused, n)
		}
	}
	sort.Strings(res.report.Unused)
	return res
}

func normalizedOrigin(o *Origin) *Origin {
	if o == nil || *o == (Origin{}) {
		return nil
	}
	c := *o
	return &c
}

func foldSet(refs []CategoryRef) map[string]bool {
	set := map[string]bool{}
	for _, r := range refs {
		set[Fold(r.Classification)+"\x00"+Fold(r.Category)] = true
	}
	return set
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// sameContent is the canonical comparison of slice 03 §1.5: stem, options in
// order, explanation, difficulty, folded category set, origin and source.
func sameContent(a, b Question) bool {
	ao, bo := normalizedOrigin(a.Origin), normalizedOrigin(b.Origin)
	if (ao == nil) != (bo == nil) || (ao != nil && *ao != *bo) {
		return false
	}
	if len(a.Options) != len(b.Options) {
		return false
	}
	for i := range a.Options {
		if a.Options[i] != b.Options[i] {
			return false
		}
	}
	return a.Type == b.Type && a.Stem == b.Stem && a.Explanation == b.Explanation && a.Difficulty == b.Difficulty &&
		a.Source == b.Source && sameSet(foldSet(a.Categories), foldSet(b.Categories))
}

// buildPlan is stage 5: inference, diff, weights and the write.
func buildPlan(in PlanInput, uses [][]classUse, reg []*regClass, primaryName string, hasPrimary bool) ImportPlan {
	qs := in.File.Questions
	primaryFold := Fold(primaryName)
	var report ImportReport
	w := &ImportWrite{ExamID: in.ExamID, Now: in.Now}

	// Classifications.
	for _, rc := range reg {
		if rc.existing {
			if hasPrimary && rc.fold == primaryFold && rc.position != PositionPrimary {
				rc.position = PositionPrimary
				w.PromoteToPrimary = rc.id
			}
			continue
		}
		rc.id = in.NewID()
		if hasPrimary && rc.fold == primaryFold {
			rc.position = PositionPrimary
		}
		report.NewClassifications = append(report.NewClassifications, rc.name)
		w.Classifications = append(w.Classifications, Classification{
			ID: rc.id, ExamID: in.ExamID, Name: rc.name, Fold: rc.fold, Position: rc.position, WeightMode: WeightInferred,
		})
	}
	if hasPrimary {
		report.Primary = &primaryName
	}

	storedByKey := map[string]Question{}
	for _, bq := range in.Bank.Questions {
		storedByKey[bq.Key] = bq
	}

	fileSource := DefaultSource
	if in.File.Source != nil {
		fileSource = *in.File.Source
	}

	type outcome struct {
		kind     string
		question Question
	}
	outcomes := make([]outcome, len(qs))
	incomingFP := map[string]string{} // key -> fingerprint after the import
	var createdKeys []int

	for i, fqn := range qs {
		var refs []CategoryRef
		var catIDs []string
		for _, cu := range uses[i] {
			rc := findClass(reg, cu.fold)
			for _, cat := range cu.cats {
				rk := findCat(rc, cat.fold)
				if rk == nil {
					rk = &regCat{id: in.NewID(), key: Slug(cat.name), name: cat.name, fold: cat.fold}
					nc := NewCategory{Classification: rc.name, Name: cat.name}
					var existingNames []string
					for _, k := range rc.cats {
						if k.existing {
							existingNames = append(existingNames, k.name)
						}
					}
					nc.SimilarTo = SimilarNames(cat.name, existingNames)
					rc.cats = append(rc.cats, rk)
					report.NewCategories = append(report.NewCategories, nc)
					w.Categories = append(w.Categories, Category{
						ID: rk.id, ClassificationID: rc.id, Key: rk.key, Name: rk.name, Fold: rk.fold,
					})
				}
				refs = append(refs, CategoryRef{Classification: rc.name, Category: rk.name})
				catIDs = append(catIDs, rk.id)
			}
		}

		source := fileSource
		if fqn.Source != nil {
			source = *fqn.Source
		}
		incoming := Question{
			ExamID: in.ExamID, Key: fqn.Key, Type: fqn.Type, Stem: fqn.Stem, Explanation: fqn.Explanation,
			Difficulty: fqn.Difficulty, Source: source, Origin: normalizedOrigin(fqn.Origin),
			Categories: refs, Options: append([]Option(nil), fqn.Options...), Fingerprint: StemFingerprint(fqn.Stem),
		}
		incomingFP[fqn.Key] = incoming.Fingerprint

		stored, exists := storedByKey[fqn.Key]
		switch {
		case !exists:
			incoming.ID, incoming.Version, incoming.CreatedAt, incoming.UpdatedAt = in.NewID(), 1, in.Now, in.Now
			w.Creates = append(w.Creates, QuestionWrite{Question: incoming, CategoryIDs: catIDs})
			report.Keys.Create = append(report.Keys.Create, fqn.Key)
			outcomes[i] = outcome{"create", incoming}
			createdKeys = append(createdKeys, i)
		case sameContent(stored, incoming):
			report.Keys.Unchanged = append(report.Keys.Unchanged, fqn.Key)
			outcomes[i] = outcome{"unchanged", stored}
		default:
			incoming.ID, incoming.Version, incoming.CreatedAt, incoming.UpdatedAt = stored.ID, stored.Version+1, stored.CreatedAt, in.Now
			w.Updates = append(w.Updates, QuestionWrite{Question: incoming, CategoryIDs: catIDs, ExpectedVersion: stored.Version})
			report.Keys.Update = append(report.Keys.Update, fqn.Key)
			outcomes[i] = outcome{"update", incoming}
		}
	}
	report.Counts = ImportCounts{Create: len(report.Keys.Create), Update: len(report.Keys.Update), Unchanged: len(report.Keys.Unchanged)}

	for i := 0; i < len(outcomes) && i < 5; i++ {
		report.Preview = append(report.Preview, outcomes[i].question)
	}

	// Duplicate stems: a created question against the alive bank as it will be.
	owner := map[string]string{}
	for _, bq := range in.Bank.Questions {
		fp := bq.Fingerprint
		if nfp, rewritten := incomingFP[bq.Key]; rewritten {
			fp = nfp
		}
		if _, taken := owner[fp]; !taken {
			owner[fp] = bq.Key
		}
	}
	for _, i := range createdKeys {
		if existing, dup := owner[outcomes[i].question.Fingerprint]; dup && existing != qs[i].Key {
			report.DuplicateStems = append(report.DuplicateStems, DuplicateStem{Key: qs[i].Key, ExistingKey: existing})
		}
	}

	// Warnings and weights.
	for _, nc := range report.NewCategories {
		if len(nc.SimilarTo) > 0 {
			quoted := make([]string, len(nc.SimilarTo))
			for i, s := range nc.SimilarTo {
				quoted[i] = fmt.Sprintf("%q", s)
			}
			report.Warnings = append(report.Warnings, fmt.Sprintf("%q is similar to %s", nc.Name, strings.Join(quoted, ", ")))
		}
	}
	after := in.Bank.After(*w)
	var primary *Classification
	for i := range after.Classifications {
		if after.Classifications[i].IsPrimary() {
			primary = &after.Classifications[i]
		}
	}
	if primary != nil {
		primary.WeightMode = primaryMode(reg, primary.ID)
		if primary.WeightMode == WeightManual {
			for _, c := range primary.Categories {
				report.WeightsAfter = append(report.WeightsAfter, WeightAfter{Category: c.Name, Percent: c.WeightPercent, Mode: WeightManual})
			}
			for _, nc := range w.Categories {
				if nc.ClassificationID == primary.ID {
					report.Warnings = append(report.Warnings, fmt.Sprintf("%q is not part of the full simulation until you assign it a weight", nc.Name))
				}
			}
		} else {
			for ci, cw := range InferWeights(primary.Categories, after.LinkCounts()) {
				report.WeightsAfter = append(report.WeightsAfter, WeightAfter{Category: primary.Categories[ci].Name, Percent: cw.Percent, Mode: WeightInferred})
				w.Weights = append(w.Weights, cw)
			}
		}
	} else if len(reg) > 0 {
		report.Warnings = append(report.Warnings,
			"could not choose a primary classification: every one has questions with more than one value; choose one with the primary parameter")
	}

	return ImportPlan{Report: report, Write: w}
}

// primaryMode is the weight mode of the primary: the stored one for an
// existing primary, inferred for one that becomes primary now.
func primaryMode(reg []*regClass, id string) WeightMode {
	for _, rc := range reg {
		if rc.id == id && rc.existing && rc.mode != "" {
			return rc.mode
		}
	}
	return WeightInferred
}
