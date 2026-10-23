package domain

import (
	"slices"
	"strings"
	"testing"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestIsMediaSHAAcceptsExactly64LowercaseHexCharacters(t *testing.T) { // BR-Q4, slice 05 §1.3
	for name, tc := range map[string]struct {
		in   string
		want bool
	}{
		"64 lowercase hex":     {shaA, true},
		"mixed digits and hex": {strings.Repeat("0123456789abcdef", 4), true},
		"63 characters":        {shaA[:63], false},
		"65 characters":        {shaA + "a", false},
		"uppercase hex":        {strings.ToUpper(shaA), false},
		"non-hex letter":       {shaA[:63] + "g", false},
		"with the scheme":      {"media:" + shaA, false},
		"a path":               {"img/r101.png", false},
		"empty":                {"", false},
	} {
		if got := IsMediaSHA(tc.in); got != tc.want {
			t.Errorf("%s: IsMediaSHA(%q) = %v, want %v", name, tc.in, got, tc.want)
		}
	}
}

func TestMediaRefsFindsEveryPlaceInDocumentOrderWithItsPath(t *testing.T) { // slice 05 §1.3 "stems, explanations, rationales and option.image"
	q := fq("m")
	q.Stem = "Look ![Señal](media:img/r101.png) and ![again](media:img/r101.png)"
	q.Explanation = "See ![x](media:img/expl.png)"
	q.Options[0].Image, q.Options[0].Alt = "media:img/opt.png", "alt"
	q.Options[1].Rationale = "![r](media:img/why.png)"
	q.Options[2].Text = "text with ![t](media:img/in-text.png)"
	other := fq("n")
	other.Stem = "![s](media:" + shaA + ")"

	got := MediaRefs([]FileQuestion{q, other})

	want := []MediaRefAt{
		{"questions[0].stem", "img/r101.png"},
		{"questions[0].stem", "img/r101.png"},
		{"questions[0].explanation", "img/expl.png"},
		{"questions[0].options[0].image", "img/opt.png"},
		{"questions[0].options[1].rationale", "img/why.png"},
		{"questions[0].options[2].text", "img/in-text.png"},
		{"questions[1].stem", shaA},
	}
	if !slices.Equal(got, want) {
		t.Errorf("MediaRefs =\n%v\nwant\n%v", got, want)
	}
}

func TestMediaRefsIgnoresPlainTextAndOtherLinks(t *testing.T) { // the ref regex
	q := fq("m")
	q.Stem = "social media: Twitter, [a link](https://example.com/media:x.png) and ![i](https://example.com/a.png)"
	q.Options[0].Image, q.Options[0].Alt = "https://example.com/a.png", "alt" // not media: -> not a reference

	if got := MediaRefs([]FileQuestion{q}); len(got) != 0 {
		t.Errorf("MediaRefs = %v, want none", got)
	}
}

func TestMediaRefsUnderstandsAngleBracketsTitlesAndSpacing(t *testing.T) { // the ref regex
	q := fq("m")
	q.Stem = `![a](<media:img/angle.png>) ![b](media:img/titled.png "A title") ![c](  media:img/spaced.png)`

	var refs []string
	for _, r := range MediaRefs([]FileQuestion{q}) {
		refs = append(refs, r.Ref)
	}

	if want := []string{"img/angle.png", "img/titled.png", "img/spaced.png"}; !slices.Equal(refs, want) {
		t.Errorf("refs = %v, want %v", refs, want)
	}
}

func rewriteTo(table map[string]string) func(string) (string, bool) {
	return func(ref string) (string, bool) { v, ok := table[ref]; return v, ok }
}

func TestRewriteMediaMarkdownReplacesOnlyTheTargetAndKeepsTheRest(t *testing.T) { // import direction
	in := `A ![x](media:img/a.png) B ![y](<media:img/b.png>) C ![z](media:img/c.png "T") D ![k](media:img/unknown.png) E ![w](https://x/y.png)`
	fn := rewriteTo(map[string]string{"img/a.png": shaA, "img/b.png": shaB, "img/c.png": shaA})

	got := RewriteMediaMarkdown(in, fn)

	want := `A ![x](media:` + shaA + `) B ![y](<media:` + shaB + `>) C ![z](media:` + shaA + ` "T") D ![k](media:img/unknown.png) E ![w](https://x/y.png)`
	if got != want {
		t.Errorf("RewriteMediaMarkdown =\n%s\nwant\n%s", got, want)
	}
}

func TestRewriteFileMediaRewritesEveryPlaceAndLeavesTheInputUntouched(t *testing.T) { // import direction
	q := fq("m")
	q.Stem = "![s](media:img/a.png)"
	q.Explanation = "![e](media:img/a.png)"
	q.Options[0].Image, q.Options[0].Alt = "media:img/a.png", "alt"
	q.Options[1].Rationale = "![r](media:img/a.png)"
	q.Options[2].Text = "![t](media:img/a.png)"
	in := []FileQuestion{q}

	got := RewriteFileMedia(in, rewriteTo(map[string]string{"img/a.png": shaA}))

	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	ref := "media:" + shaA
	g := got[0]
	if g.Stem != "![s]("+ref+")" || g.Explanation != "![e]("+ref+")" || g.Options[0].Image != ref ||
		g.Options[1].Rationale != "![r]("+ref+")" || g.Options[2].Text != "![t]("+ref+")" {
		t.Errorf("rewritten question = %+v", g)
	}
	if in[0].Stem != "![s](media:img/a.png)" || in[0].Options[0].Image != "media:img/a.png" {
		t.Errorf("the input was modified: %+v", in[0])
	}
}

func TestRewriteFileMediaKeepsAnImageThatIsNotAMediaReference(t *testing.T) {
	q := fq("m")
	q.Options[0].Image, q.Options[0].Alt = "https://example.com/a.png", "alt"

	got := RewriteFileMedia([]FileQuestion{q}, func(string) (string, bool) { return "x", true })

	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	if got[0].Options[0].Image != "https://example.com/a.png" {
		t.Errorf("image = %q", got[0].Options[0].Image)
	}
}

func TestRewriteQuestionMediaTurnsStoredHashesIntoExportPaths(t *testing.T) { // export direction, BR-Q4
	stored := Question{Key: "m", Stem: "![s](media:" + shaA + ")", Explanation: "plain",
		Options: []Option{{Image: "media:" + shaB, Alt: "alt"}, {Text: "t", Rationale: "![r](media:" + shaA + ")"}}}

	got := RewriteQuestionMedia(stored, rewriteTo(map[string]string{shaA: "img/" + shaA + ".png", shaB: "img/" + shaB + ".webp"}))

	if len(got.Options) != 2 || got.Stem != "![s](media:img/"+shaA+".png)" || got.Options[0].Image != "media:img/"+shaB+".webp" ||
		got.Options[1].Rationale != "![r](media:img/"+shaA+".png)" || got.Explanation != "plain" {
		t.Errorf("rewritten = %+v", got)
	}
	if stored.Stem != "![s](media:"+shaA+")" || stored.Options[0].Image != "media:"+shaB {
		t.Errorf("the stored question was modified: %+v", stored)
	}
}

func TestMediaExtensionMapsTheThreeStoredTypes(t *testing.T) { // FR-6
	for in, want := range map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"} {
		if got, ok := MediaExtension(in); !ok || got != want {
			t.Errorf("MediaExtension(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := MediaExtension("image/svg+xml"); ok {
		t.Error("svg has an extension")
	}
}

func TestQuestionMediaHashesListsEachStoredHashOnceInOrderOfFirstUse(t *testing.T) { // export: which files to put in img/
	a := Question{Stem: "![x](media:" + shaB + ") and ![y](media:" + shaA + ")", Options: []Option{{Image: "media:" + shaA, Alt: "a"}, {Text: "plain"}}}
	b := Question{Explanation: "![z](media:" + shaB + ")", Options: []Option{{Text: "t", Rationale: "social media: no"}}}

	got := QuestionMediaHashes([]Question{a, b})

	if want := []string{shaB, shaA}; !slices.Equal(got, want) {
		t.Errorf("QuestionMediaHashes = %v, want %v", got, want)
	}
	if len(QuestionMediaHashes(nil)) != 0 {
		t.Error("no questions, no hashes")
	}
}
