package services

import (
	"html"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Text helpers shared by search and the article reader. Folding here mirrors
// SQLite's `unicode61 remove_diacritics 2` tokenizer, so what FTS5 matches is
// what we highlight.

var (
	tagRe     = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe   = regexp.MustCompile(`[ \t\r\f\v]+`)
	tokenRe   = regexp.MustCompile(`[\p{L}\p{N}]+`)
	sentEndRe = regexp.MustCompile(`([.!?…])\s+`)
)

// maxTerms bounds how many distinct words one query contributes, so a pasted
// paragraph can't turn into a thousand-clause MATCH.
const maxTerms = 12

// StripHTML removes tags and resolves entities, keeping line structure intact.
func StripHTML(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// foldRune reduces a rune to its lowercase, diacritic-free base. Returns false
// for runes that fold to nothing (combining marks).
func foldRune(r rune) (rune, bool) {
	if unicode.Is(unicode.Mn, r) {
		return 0, false
	}
	for _, c := range norm.NFD.String(string(r)) {
		if unicode.Is(unicode.Mn, c) {
			continue
		}
		return unicode.ToLower(c), true
	}
	return 0, false
}

// foldIndexed folds s and returns, alongside the folded runes, the byte offset
// in s of each one plus a trailing sentinel of len(s). The offsets let a match
// found in folded space be mapped back onto the original bytes.
func foldIndexed(s string) ([]rune, []int) {
	folded := make([]rune, 0, len(s))
	offsets := make([]int, 0, len(s)+1)
	for i, r := range s {
		if f, ok := foldRune(r); ok {
			folded = append(folded, f)
			offsets = append(offsets, i)
		}
	}
	offsets = append(offsets, len(s))
	return folded, offsets
}

// Fold returns the lowercase, diacritic-free form of s.
func Fold(s string) string {
	f, _ := foldIndexed(s)
	return string(f)
}

// Tokens extracts the distinct searchable words from a raw user query, folded.
func Tokens(q string) []string {
	out := make([]string, 0, maxTerms)
	seen := make(map[string]bool)
	for _, raw := range tokenRe.FindAllString(q, -1) {
		t := Fold(raw)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == maxTerms {
			break
		}
	}
	return out
}

// Matches finds every occurrence of any term in s, accent- and case-insensitively,
// as byte ranges into s, sorted and with overlaps merged.
func Matches(s string, terms []string) [][2]int {
	if len(terms) == 0 || s == "" {
		return nil
	}
	folded, offsets := foldIndexed(s)
	var found [][2]int
	for _, term := range terms {
		tr := []rune(term)
		if len(tr) == 0 || len(tr) > len(folded) {
			continue
		}
		for i := 0; i+len(tr) <= len(folded); i++ {
			// FTS5 matches whole tokens (with a prefix wildcard on the last
			// one), so a match must start a word. Without this, "de" lights up
			// inside "acorde" and the highlighting stops meaning anything.
			if i > 0 && isWordRune(folded[i-1]) {
				continue
			}
			hit := true
			for j, c := range tr {
				if folded[i+j] != c {
					hit = false
					break
				}
			}
			if hit {
				found = append(found, [2]int{offsets[i], offsets[i+len(tr)]})
			}
		}
	}
	return mergeRanges(found)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func mergeRanges(in [][2]int) [][2]int {
	if len(in) < 2 {
		return in
	}
	// insertion sort by start — match counts are small and already near-sorted
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j][0] < in[j-1][0]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	out := in[:1]
	for _, r := range in[1:] {
		last := &out[len(out)-1]
		if r[0] <= last[1] {
			if r[1] > last[1] {
				last[1] = r[1]
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// Highlight HTML-escapes s and wraps every term occurrence in <mark>. The
// escaping is what makes it safe to render stored article and flashcard text.
func Highlight(s string, terms []string) string {
	var b strings.Builder
	prev := 0
	for _, m := range Matches(s, terms) {
		b.WriteString(html.EscapeString(s[prev:m[0]]))
		b.WriteString("<mark>")
		b.WriteString(html.EscapeString(s[m[0]:m[1]]))
		b.WriteString("</mark>")
		prev = m[1]
	}
	b.WriteString(html.EscapeString(s[prev:]))
	return b.String()
}

// bestWindow picks which match to build the snippet around: the one whose
// surrounding window carries the most matched characters. Anchoring on the
// first match instead puts the excerpt wherever a short common word happens to
// appear first ("de" in the page chrome), which tells the reader nothing.
// Scoring by matched length favours the long, rare words in the query.
func bestWindow(s string, matches [][2]int, width int) int {
	// A rune-width window in bytes, generously — Spanish text averages well
	// under 2 bytes/rune, and overshooting only widens the scoring window.
	span := width * 2

	bestStart, bestScore := matches[0][0], -1
	for _, anchor := range matches {
		from := anchor[0] - span/3
		to := from + span
		score := 0
		for _, m := range matches {
			if m[0] >= from && m[1] <= to {
				score += m[1] - m[0]
			}
		}
		if score > bestScore {
			bestScore, bestStart = score, anchor[0]
		}
	}
	return bestStart
}

// Snippet returns an escaped, <mark>-highlighted window of about width runes
// centred on the densest cluster of matches. Falls back to the head of the text
// when nothing matches (a hit in the title, say, with none in the body).
func Snippet(text string, terms []string, width int) string {
	clean := strings.Join(strings.Fields(StripHTML(text)), " ")
	if clean == "" {
		return ""
	}
	runes := []rune(clean)
	if len(runes) <= width {
		return Highlight(clean, terms)
	}

	start := 0
	if ms := Matches(clean, terms); len(ms) > 0 {
		// byte offset → rune index
		hit := len([]rune(clean[:bestWindow(clean, ms, width)]))
		start = hit - width/3
	}
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(runes) {
		end = len(runes)
		start = end - width
	}
	// don't cut mid-word
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}

	out := Highlight(string(runes[start:end]), terms)
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// SplitParagraphs turns stored article text into readable paragraphs. Readeck
// keeps line breaks, so those win; text scraped by our own HTML extractor
// arrives as one long space-joined blob and gets chunked by sentence instead.
func SplitParagraphs(text string) []string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	if text == "" {
		return nil
	}

	if strings.Contains(text, "\n") {
		var out []string
		for _, block := range strings.Split(text, "\n") {
			block = strings.TrimSpace(spaceRe.ReplaceAllString(block, " "))
			if block != "" {
				out = append(out, block)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	const targetRunes = 600
	sentences := sentEndRe.Split(text, -1)
	marks := sentEndRe.FindAllStringSubmatch(text, -1)

	var out []string
	var cur strings.Builder
	for i, s := range sentences {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if i < len(marks) {
			s += marks[i][1]
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(s)
		if len([]rune(cur.String())) >= targetRunes {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
