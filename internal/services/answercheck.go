package services

import "strings"

// FSRS grades, named so the intent of a pre-selected rating is readable.
const (
	RatingAgain = 1
	RatingHard  = 2
	RatingGood  = 3
	RatingEasy  = 4
)

// Diff token statuses.
const (
	DiffSame    = "same"
	DiffMissing = "missing"
	DiffExtra   = "extra"
)

// DiffToken is one word of the comparison between what the learner produced and
// what the card stores. Missing tokens carry the stored answer's spelling; extra
// ones carry the learner's.
type DiffToken struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// AnswerCheck is the system's observation of a production attempt. It is an
// observation, not a verdict: the rating it suggests is a starting point the
// learner can override, because a synonym or a differently-worded but correct
// answer is theirs to judge, not ours.
type AnswerCheck struct {
	Typed           string      `json:"typed"`
	Expected        string      `json:"expected"`
	Match           bool        `json:"match"`
	Diff            []DiffToken `json:"diff"`
	SuggestedRating int         `json:"suggested_rating"`
}

// CheckAnswer compares a typed answer against a card's stored back. Comparison
// ignores case, diacritics, punctuation and whitespace runs, so a correct recall
// is never scored as a failure over a missing tilde.
func CheckAnswer(typed, back string) AnswerCheck {
	expected := StripHTML(back)

	typedWords := splitWords(typed)
	expectedWords := splitWords(expected)

	check := AnswerCheck{
		Typed:    strings.TrimSpace(typed),
		Expected: expected,
		Diff:     diffWords(expectedWords, typedWords),
	}

	check.Match = equalWords(expectedWords, typedWords)
	if check.Match {
		check.SuggestedRating = RatingGood
	} else {
		check.SuggestedRating = RatingAgain
	}
	return check
}

// word carries both forms: the one to show and the one to compare on.
type word struct {
	display string
	key     string
}

func splitWords(s string) []word {
	out := []word{}
	for _, field := range strings.Fields(s) {
		key := stripPunctuation(Fold(field))
		if key == "" {
			continue
		}
		out = append(out, word{display: field, key: key})
	}
	return out
}

// stripPunctuation keeps letters, digits and the marks that live inside words in
// the languages Recall stores, so "método!" and "método" compare equal while
// "rock'n'roll" stays one token.
func stripPunctuation(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '\'':
			b.WriteRune(r)
		default:
			if isWordRune(r) {
				b.WriteRune(r)
			}
		}
	}
	return strings.Trim(b.String(), "'")
}

func equalWords(a, b []word) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].key != b[i].key {
			return false
		}
	}
	return true
}

// diffWords walks the longest common subsequence of the two word lists and emits
// a single merged sequence: shared words once, words only in the stored answer
// as missing, words only in the typed answer as extra.
func diffWords(expected, typed []word) []DiffToken {
	lcs := lcsTable(expected, typed)

	out := []DiffToken{}
	i, j := 0, 0
	for i < len(expected) && j < len(typed) {
		if expected[i].key == typed[j].key {
			out = append(out, DiffToken{Text: expected[i].display, Status: DiffSame})
			i++
			j++
			continue
		}
		if lcs[i+1][j] >= lcs[i][j+1] {
			out = append(out, DiffToken{Text: expected[i].display, Status: DiffMissing})
			i++
			continue
		}
		out = append(out, DiffToken{Text: typed[j].display, Status: DiffExtra})
		j++
	}
	for ; i < len(expected); i++ {
		out = append(out, DiffToken{Text: expected[i].display, Status: DiffMissing})
	}
	for ; j < len(typed); j++ {
		out = append(out, DiffToken{Text: typed[j].display, Status: DiffExtra})
	}
	return out
}

// lcsTable[i][j] is the length of the longest common subsequence of the suffixes
// expected[i:] and typed[j:].
func lcsTable(expected, typed []word) [][]int {
	table := make([][]int, len(expected)+1)
	for i := range table {
		table[i] = make([]int, len(typed)+1)
	}
	for i := len(expected) - 1; i >= 0; i-- {
		for j := len(typed) - 1; j >= 0; j-- {
			if expected[i].key == typed[j].key {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	return table
}
