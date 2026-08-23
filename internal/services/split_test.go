package services

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// ISC-63 — the prompt states the minimum-information principle, and the five
// list-formatting rules that used to occupy it are gone. The example matters as
// much as the rules: the old one demonstrated a multi-fact back inside <ul><li>,
// and models copy the shape they are shown.
func TestDefaultPromptStatesMinimumInformation(t *testing.T) {
	p := DefaultFlashcardPrompt

	for _, want := range []string{
		"MINIMUM INFORMATION",
		"one idea",
		"N cards",
		"conjunction",
		"BOTH DIRECTIONS",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt no longer states %q", want)
		}
	}

	for _, banned := range []string{
		"<ul><li>",
		"<ol><li>",
		"use <ul>",
		"proper HTML list tags",
	} {
		if strings.Contains(p, banned) {
			t.Errorf("the prompt still instructs list markup: %q", banned)
		}
	}

	// It must forbid the tags rather than merely not mention them.
	if !strings.Contains(p, "Never <ul>, <ol> or <li>") {
		t.Error("the prompt does not forbid list markup outright")
	}
}

// ISC-64 — the generator's judgement about the answer reaches the column that
// decides how a card is asked, and an unrecognised value never does.
func TestGeneratedKindReachesTheCard(t *testing.T) {
	db := buryDB(t)
	cards := NewCardService(db)
	articleID := "art1"

	pairs := []FlashcardPair{
		{Front: "¿Quién escribió el Traité?", Back: "Lavoisier", Kind: KindProduction},
		{Front: "¿Qué es la respiración?", Back: "Una combustión lenta", Kind: KindRecognition},
		{Front: "sin kind", Back: "x"},
		{Front: "kind inventado", Back: "y", Kind: "cloze"},
	}
	if _, err := cards.CreateBatch("deck", &articleID, pairs); err != nil {
		t.Fatalf("create batch: %v", err)
	}

	want := map[string]string{
		"¿Quién escribió el Traité?": KindProduction,
		"¿Qué es la respiración?":    KindRecognition,
		"sin kind":                   KindRecognition,
		"kind inventado":             KindRecognition,
	}
	rows, err := db.Query("SELECT front, kind FROM cards WHERE article_id = 'art1'")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var front, kind string
		rows.Scan(&front, &kind)
		if want[front] != kind {
			t.Errorf("card %q stored kind %q, want %q", front, kind, want[front])
		}
		seen++
	}
	if seen != 4 {
		t.Errorf("read back %d cards, want 4", seen)
	}
}

// ISC-64 — the JSON contract carries kind through the parser.
func TestParseFlashcardJSONReadsKind(t *testing.T) {
	pairs, err := parseFlashcardJSON(
		`[{"front":"¿Quién?","back":"Lavoisier","kind":"production"},{"front":"¿Qué?","back":"Algo"}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("parsed %d pairs, want 2", len(pairs))
	}
	if pairs[0].Kind != KindProduction {
		t.Errorf("first pair kind %q, want production", pairs[0].Kind)
	}
	if pairs[1].Kind != "" {
		t.Errorf("second pair kind %q, want empty so the default applies", pairs[1].Kind)
	}
}

type stubSplitter struct {
	pairs []FlashcardPair
	err   error
	calls int
}

func (s *stubSplitter) SplitCard(front, back, userID string) ([]FlashcardPair, error) {
	s.calls++
	return s.pairs, s.err
}

func cardTableChecksum(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT id, deck_id, front, back, due, stability, difficulty, elapsed_days,
		scheduled_days, reps, lapses, state, last_review, created_at, updated_at, kind, suspended
		FROM cards ORDER BY id`)
	if err != nil {
		t.Fatalf("checksum: %v", err)
	}
	defer rows.Close()
	h := sha256.New()
	for rows.Next() {
		cols := make([]interface{}, 17)
		vals := make([]sql.RawBytes, 17)
		for i := range vals {
			cols[i] = &vals[i]
		}
		if err := rows.Scan(cols...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		for _, v := range vals {
			h.Write(v)
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func splitDB(t *testing.T) (*sql.DB, *SplitService) {
	t.Helper()
	db := buryDB(t)
	exec := func(id, front, back string) {
		mustExec(t, db, `
			INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
				scheduled_days, reps, lapses, state, last_review, created_at, updated_at, article_id)
			VALUES (?, 'deck', ?, ?, '2026-01-01T00:00:00Z', 3, 5, 1, 1, 4, 0, 2,
				'2026-08-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'art1')`,
			id, front, back)
	}
	exec("listy", "¿Cuál fue el legado de Lavoisier?",
		"<ul><li>Sistema métrico</li><li>Nomenclatura química</li><li>Respiración animal</li></ul>")
	exec("compound", "¿Quién fue Lavoisier y qué demostró?", "Un químico. Demostró la combustión lenta.")
	exec("fine", "¿Quién escribió el Traité?", "<strong>Lavoisier</strong>")
	return db, NewSplitService(db)
}

// ISC-66 — the splitter finds every malformed card and only those, using the
// same two detectors `recall metrics` counts with.
func TestSplitterFindsExactlyTheMalformedCards(t *testing.T) {
	db, splits := splitDB(t)
	_ = db

	candidates, err := splits.Candidates("u1")
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	got := map[string]string{}
	for _, c := range candidates {
		got[c.Card.ID] = c.Reason
	}
	if len(got) != 2 {
		t.Fatalf("found %d candidates (%v), want listy and compound", len(got), got)
	}
	if !strings.Contains(got["listy"], "list") {
		t.Errorf("listy reason is %q", got["listy"])
	}
	if !strings.Contains(got["compound"], "conjunction") {
		t.Errorf("compound reason is %q", got["compound"])
	}
	if _, ok := got["fine"]; ok {
		t.Error("a well-formed card was proposed for splitting")
	}
}

// ISC-67 — a dry run leaves the card table byte-identical. Cards accumulated
// over months are the operator's own writing; the proposal has to be free.
func TestDryRunLeavesTheCardTableUntouched(t *testing.T) {
	db, splits := splitDB(t)
	stub := &stubSplitter{pairs: []FlashcardPair{
		{Front: "¿Qué construyó Lavoisier?", Back: "El sistema métrico"},
		{Front: "¿Qué reformó Lavoisier?", Back: "La nomenclatura química"},
	}}

	before := cardTableChecksum(t, db)

	candidates, err := splits.Candidates("u1")
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	proposed := splits.Propose("u1", candidates, stub)
	if len(proposed) != 2 || len(proposed[0].Proposed) != 2 {
		t.Fatalf("proposal did not fill in: %+v", proposed)
	}
	if stub.calls != 2 {
		t.Errorf("splitter called %d times, want one per candidate", stub.calls)
	}

	if after := cardTableChecksum(t, db); after != before {
		t.Errorf("a dry run wrote to the card table\nbefore %s\nafter  %s", before, after)
	}
}

// ISC-66 — applying names one card. It becomes its atomic parts, the original
// is suspended rather than deleted, and no other card is touched.
func TestApplySplitsOneNamedCardAndKeepsTheOriginal(t *testing.T) {
	db, splits := splitDB(t)
	tags := NewTagService(db)
	if err := tags.Attach("u1", "listy", "humanidades/ciencia"); err != nil {
		t.Fatalf("attach: %v", err)
	}

	pairs := []FlashcardPair{
		{Front: "¿Qué ayudó a construir Lavoisier?", Back: "El sistema métrico", Kind: KindRecognition},
		{Front: "¿Quién reformó la nomenclatura química?", Back: "Lavoisier", Kind: KindProduction},
	}
	n, err := splits.Apply("u1", "listy", pairs)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if n != 2 {
		t.Fatalf("created %d cards, want 2", n)
	}

	// The original is out of rotation but still there, with its history.
	var suspended bool
	var reps int
	if err := db.QueryRow("SELECT suspended, reps FROM cards WHERE id = 'listy'").Scan(&suspended, &reps); err != nil {
		t.Fatalf("original gone: %v", err)
	}
	if !suspended {
		t.Error("the original card is still being served")
	}
	if reps != 4 {
		t.Errorf("the original lost its history: reps = %d", reps)
	}

	// The compound card, which was not named, is untouched — and in particular
	// it did not inherit the original's tag just for being untagged.
	var otherSuspended bool
	db.QueryRow("SELECT suspended FROM cards WHERE id = 'compound'").Scan(&otherSuspended)
	if otherSuspended {
		t.Error("a card the operator did not name was modified")
	}
	for _, untouched := range []string{"compound", "fine"} {
		if got, _ := tags.ForCard(untouched); len(got) != 0 {
			t.Errorf("%s picked up %+v from a split it was not part of", untouched, got)
		}
	}

	// The atomic cards carry the kind the proposal gave them, and inherit the
	// original's topic so a split does not drop the material out of its tag.
	rows, _ := db.Query("SELECT id, kind FROM cards WHERE id NOT IN ('listy','compound','fine')")
	defer rows.Close()
	found := 0
	for rows.Next() {
		var id, kind string
		rows.Scan(&id, &kind)
		found++
		got, err := tags.ForCard(id)
		if err != nil {
			t.Fatalf("tags: %v", err)
		}
		if len(got) != 1 || got[0].Key != "humanidades/ciencia" {
			t.Errorf("new card %s carries %+v, want the original's tag", id, got)
		}
	}
	if found != 2 {
		t.Errorf("found %d atomic cards, want 2", found)
	}
}

// ISC-66 — applying with nothing proposed refuses rather than suspending the
// original and leaving the learner with neither card.
func TestApplyRefusesAnEmptyProposal(t *testing.T) {
	db, splits := splitDB(t)
	before := cardTableChecksum(t, db)

	if _, err := splits.Apply("u1", "listy", nil); err == nil {
		t.Fatal("an empty proposal was applied")
	}
	if after := cardTableChecksum(t, db); after != before {
		t.Error("a refused apply still wrote to the card table")
	}
}

// ISC-66 — a card belonging to someone else cannot be split.
func TestApplyStopsAtTheOwner(t *testing.T) {
	db, splits := splitDB(t)
	mustExec(t, db, `
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at)
		VALUES ('theirs', 'otherdeck', 'q y w', '<ul><li>a</li></ul>', '2026-01-01T00:00:00Z', 0,0,0,0,0,0,2,
			'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)

	if _, err := splits.Apply("u1", "theirs", []FlashcardPair{{Front: "a", Back: "b"}}); err == nil {
		t.Fatal("split another user's card")
	}
	var suspended bool
	db.QueryRow("SELECT suspended FROM cards WHERE id = 'theirs'").Scan(&suspended)
	if suspended {
		t.Error("another user's card was suspended")
	}
	_ = fmt.Sprintf("")
}
