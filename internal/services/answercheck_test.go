package services

import "testing"

// ISC-30 — the comparison ignores case, accents, punctuation and surrounding
// whitespace, so a learner who produced the answer is not told they failed
// because of a missing tilde.
func TestCheckAnswerIgnoresCaseAccentsAndPunctuation(t *testing.T) {
	cases := []struct {
		name     string
		typed    string
		back     string
		wantMatch bool
	}{
		{"exact", "Verdad y método", "Verdad y método", true},
		{"case", "VERDAD Y MÉTODO", "Verdad y método", true},
		{"accents dropped", "Fenomenologia del espiritu", "Fenomenología del Espíritu", true},
		{"accents added", "Fenomenología del Espíritu", "Fenomenologia del espiritu", true},
		{"punctuation", "  ¡Verdad, y método!  ", "Verdad y método", true},
		{"inner whitespace", "Verdad    y\tmétodo", "Verdad y método", true},
		{"html in the stored answer", "Verdad y método", "<strong>Verdad</strong> y <em>método</em>", true},
		{"entities in the stored answer", "Verdad & método", "Verdad &amp; método", true},
		{"wrong word", "Verdad y sentido", "Verdad y método", false},
		{"missing word", "Verdad", "Verdad y método", false},
		{"extra word", "Verdad y método hermenéutico", "Verdad y método", false},
		{"empty", "", "Verdad y método", false},
		{"whitespace only", "   ", "Verdad y método", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckAnswer(tc.typed, tc.back)
			if got.Match != tc.wantMatch {
				t.Errorf("CheckAnswer(%q, %q).Match = %v, want %v", tc.typed, tc.back, got.Match, tc.wantMatch)
			}
		})
	}
}

// ISC-31 — a miss pre-selects Again, a match pre-selects Good. The system never
// nudges a non-match toward a passing grade.
func TestCheckAnswerSuggestsAgainOnMiss(t *testing.T) {
	if got := CheckAnswer("Verdad y método", "Verdad y método"); got.SuggestedRating != RatingGood {
		t.Errorf("match suggests %d, want %d (Good)", got.SuggestedRating, RatingGood)
	}
	if got := CheckAnswer("no idea", "Verdad y método"); got.SuggestedRating != RatingAgain {
		t.Errorf("miss suggests %d, want %d (Again)", got.SuggestedRating, RatingAgain)
	}
	if got := CheckAnswer("", "Verdad y método"); got.SuggestedRating != RatingAgain {
		t.Errorf("empty suggests %d, want %d (Again)", got.SuggestedRating, RatingAgain)
	}
}

// ISC-30 — the answer view shows the typed answer against the expected one with
// the differences marked, so the learner can see exactly what was missed.
func TestCheckAnswerMarksDifferences(t *testing.T) {
	got := CheckAnswer("Verdad y sentido", "Verdad y método")

	var same, missing, extra []string
	for _, tok := range got.Diff {
		switch tok.Status {
		case DiffSame:
			same = append(same, tok.Text)
		case DiffMissing:
			missing = append(missing, tok.Text)
		case DiffExtra:
			extra = append(extra, tok.Text)
		default:
			t.Fatalf("unknown diff status %q", tok.Status)
		}
	}

	if len(same) != 2 || same[0] != "Verdad" || same[1] != "y" {
		t.Errorf("same = %v, want [Verdad y]", same)
	}
	if len(missing) != 1 || missing[0] != "método" {
		t.Errorf("missing = %v, want [método]", missing)
	}
	if len(extra) != 1 || extra[0] != "sentido" {
		t.Errorf("extra = %v, want [sentido]", extra)
	}
}

// The diff keeps the stored answer's own spelling for the words the learner got
// right, rather than echoing back whatever the learner typed.
func TestCheckAnswerDiffUsesStoredSpelling(t *testing.T) {
	got := CheckAnswer("fenomenologia del espiritu", "Fenomenología del Espíritu")

	if !got.Match {
		t.Fatalf("expected a match")
	}
	for _, tok := range got.Diff {
		if tok.Status != DiffSame {
			t.Fatalf("expected every token to be same, got %q for %q", tok.Status, tok.Text)
		}
	}
	if len(got.Diff) != 3 || got.Diff[0].Text != "Fenomenología" || got.Diff[2].Text != "Espíritu" {
		t.Errorf("diff = %+v, want the stored spelling", got.Diff)
	}
}

// An empty answer marks every expected word as missing, which is what makes the
// answer view useful rather than blank.
func TestCheckAnswerEmptyMarksEverythingMissing(t *testing.T) {
	got := CheckAnswer("", "Verdad y método")

	if got.Match {
		t.Error("empty answer must not match")
	}
	if len(got.Diff) != 3 {
		t.Fatalf("diff has %d tokens, want 3", len(got.Diff))
	}
	for _, tok := range got.Diff {
		if tok.Status != DiffMissing {
			t.Errorf("token %q is %q, want missing", tok.Text, tok.Status)
		}
	}
}

// Expected is the stripped form of the stored answer, so the template can show
// it next to the typed text without leaking markup.
func TestCheckAnswerExposesStrippedExpected(t *testing.T) {
	got := CheckAnswer("x", "<ul><li>Uno</li><li>Dos</li></ul>")

	if got.Expected != "Uno Dos" {
		t.Errorf("Expected = %q, want %q", got.Expected, "Uno Dos")
	}
}
