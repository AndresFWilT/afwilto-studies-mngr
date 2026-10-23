package domain

import (
	"fmt"
	"strings"
	"testing"
)

// Phase-1 review H2: a report holds at most 200 errors and says how many it left out.

func manyErrors(n int) []ImportError {
	errs := make([]ImportError, n)
	for i := range errs {
		errs[i] = ImportError{Path: fmt.Sprintf("questions[%d].key", i), Rule: RuleSchema, Message: "boom"}
	}
	return errs
}

func TestPlanFailureCapsTheReportAtTwoHundredErrorsAndSaysHowManyWereLeftOut(t *testing.T) { // H2
	report := PlanFailure(manyErrors(1000)).Report

	if len(report.Errors) != 200 {
		t.Fatalf("errors = %d, want 200", len(report.Errors))
	}
	if report.Errors[0].Path != "questions[0].key" || report.Errors[199].Path != "questions[199].key" {
		t.Errorf("the cap must keep the first errors in order: %q .. %q", report.Errors[0].Path, report.Errors[199].Path)
	}
	if !hasWarning(report.Warnings, "800 more errors not shown") {
		t.Errorf("warnings = %q, want %q", report.Warnings, "800 more errors not shown")
	}
	if report.Counts.Invalid != 1000 {
		t.Errorf("invalid = %d, want 1000: the count is of every invalid question, shown or not", report.Counts.Invalid)
	}
}

func TestPlanFailureAtExactlyTheCapAddsNoWarning(t *testing.T) { // H2 boundary
	for _, n := range []int{0, 1, 199, 200} {
		report := PlanFailure(manyErrors(n)).Report
		if len(report.Errors) != n || len(report.Warnings) != 0 {
			t.Errorf("%d errors: shown %d, warnings %q; want all shown and no warning", n, len(report.Errors), report.Warnings)
		}
	}
	report := PlanFailure(manyErrors(201)).Report
	if len(report.Errors) != 200 || !hasWarning(report.Warnings, "1 more errors not shown") {
		t.Errorf("201 errors: shown %d, warnings %q", len(report.Errors), report.Warnings)
	}
}

func TestPlanImportSemanticErrorsAreCappedToo(t *testing.T) { // H2: every stage, not only the schema
	qs := make([]FileQuestion, 300)
	for i := range qs {
		q := fq(fmt.Sprintf("q%03d", i))
		q.Options[1].Correct = false // single question without a correct option: correct_count
		qs[i] = q
	}

	plan := planOf(t, Bank{}, file(qs...), "")

	if len(plan.Report.Errors) != 200 || plan.Write != nil {
		t.Fatalf("errors = %d (write nil: %v), want 200 and no write", len(plan.Report.Errors), plan.Write == nil)
	}
	if !hasWarning(plan.Report.Warnings, "100 more errors not shown") {
		t.Errorf("warnings = %q", plan.Report.Warnings)
	}
}

func hasWarning(warnings []string, want string) bool {
	for _, w := range warnings {
		if strings.Contains(w, want) {
			return true
		}
	}
	return false
}
