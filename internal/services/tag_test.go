package services

import (
	"database/sql"
	"testing"
	"time"
)

// ISC-54 — two spellings of one tag normalize to one key. This is the whole of
// the orthographic fix, and every other tag claim rests on it.
func TestNormalizeTagKeyCollapsesSpellings(t *testing.T) {
	same := []string{
		"musica/teoria-armonica",
		"Música/Teoría Armónica",
		"MUSICA / TEORIA  ARMONICA",
		"música/teoría—armónica",
		"  Música/teoría_armónica  ",
	}
	want := NormalizeTagKey(same[0])
	if want != "musica/teoria-armonica" {
		t.Fatalf("baseline key is %q", want)
	}
	for _, raw := range same[1:] {
		if got := NormalizeTagKey(raw); got != want {
			t.Errorf("NormalizeTagKey(%q) = %q, want %q", raw, got, want)
		}
	}
}

// ISC-54 — the shape is enforced, and an unknown domain is refused rather than
// created. A caller that could invent a first segment would grow the root one
// article at a time, which is the failure the closed list exists to prevent.
func TestValidateTagEnforcesShapeAndClosedRoot(t *testing.T) {
	ok := []struct{ raw, key, domain string }{
		{"musica/armonia", "musica/armonia", "musica"},
		{"Humanidades/Revolución Francesa", "humanidades/revolucion-francesa", "humanidades"},
		{"sistemas/IA", "sistemas/ia", "sistemas"},
	}
	for _, tc := range ok {
		key, domain, err := ValidateTag(tc.raw)
		if err != nil {
			t.Errorf("ValidateTag(%q) errored: %v", tc.raw, err)
			continue
		}
		if key != tc.key || domain != tc.domain {
			t.Errorf("ValidateTag(%q) = (%q, %q), want (%q, %q)", tc.raw, key, domain, tc.key, tc.domain)
		}
	}

	bad := []string{
		"musica",                // one segment: too coarse to filter on
		"musica/teoria/armonia", // three: free depth is what was rejected
		"agents/langchain",      // unknown domain, and in the wrong language
		"pkm/zettelkasten",      // an English acronym as a root
		"/armonia",              // empty domain
		"musica/",               // empty tema
		"música/…",              // tema normalizes to nothing
	}
	for _, raw := range bad {
		if _, _, err := ValidateTag(raw); err == nil {
			t.Errorf("ValidateTag(%q) was accepted and should not be", raw)
		}
	}
}

// ISC-54 — the store converges. Asking for the same tag under three spellings
// yields one row, because the unique index is on the key.
func TestEnsureTagConvergesOnOneRow(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1")
	seedUser(t, db, "u2")
	tags := NewTagService(db)

	first, err := tags.Ensure("u1", "Música/Teoría Armónica")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, spelling := range []string{"musica/teoria-armonica", "MUSICA / Teoria Armonica"} {
		got, err := tags.Ensure("u1", spelling)
		if err != nil {
			t.Fatalf("ensure %q: %v", spelling, err)
		}
		if got != first {
			t.Errorf("ensure %q created a second tag", spelling)
		}
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM tags WHERE user_id = 'u1'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("%d tag rows for one tag under three spellings", n)
	}

	// The display form keeps what the human first wrote.
	var display string
	db.QueryRow("SELECT display FROM tags WHERE id = ?", first).Scan(&display)
	if display != "Música/Teoría Armónica" {
		t.Errorf("display is %q; the accented form should survive", display)
	}

	// Vocabularies are per user: the same key for another user is another row.
	other, err := tags.Ensure("u2", "musica/teoria-armonica")
	if err != nil {
		t.Fatalf("ensure for u2: %v", err)
	}
	if other == first {
		t.Error("two users share a tag row")
	}
}

// ISC-54 — an unknown domain never reaches the database.
func TestEnsureTagRefusesToInventADomain(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1")

	if _, err := NewTagService(db).Ensure("u1", "agents/langchain"); err == nil {
		t.Fatal("an unknown domain was accepted")
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&n)
	if n != 0 {
		t.Errorf("%d tag rows written despite the refusal", n)
	}
}

// ISC-54 — attaching is idempotent and the card reads back its tags.
func TestAttachIsIdempotent(t *testing.T) {
	db := buryDB(t)
	leechSeed(t, db, "c1", "deck", 0)
	tags := NewTagService(db)

	for i := 0; i < 3; i++ {
		if err := tags.Attach("u1", "c1", "musica/armonia"); err != nil {
			t.Fatalf("attach: %v", err)
		}
	}
	if err := tags.Attach("u1", "c1", "Música/Armonía"); err != nil {
		t.Fatalf("attach accented: %v", err)
	}

	got, err := tags.ForCard("c1")
	if err != nil {
		t.Fatalf("for card: %v", err)
	}
	if len(got) != 1 || got[0].Key != "musica/armonia" || got[0].Domain != "musica" {
		t.Fatalf("card tags = %+v, want one musica/armonia", got)
	}
}

// ISC-55 — the classifier is shown what already exists under a domain, which is
// how a machine writer reuses a tema instead of coining a synonym for it.
func TestTemasInListsExistingLeaves(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "u1")
	tags := NewTagService(db)
	for _, raw := range []string{"musica/armonia", "musica/repertorio", "humanidades/ilustracion"} {
		if _, err := tags.Ensure("u1", raw); err != nil {
			t.Fatalf("ensure %q: %v", raw, err)
		}
	}

	temas, err := tags.TemasIn("u1", "musica")
	if err != nil {
		t.Fatalf("temas: %v", err)
	}
	if len(temas) != 2 || temas[0] != "armonia" || temas[1] != "repertorio" {
		t.Errorf("temas = %v, want [armonia repertorio]", temas)
	}
}

// stubClassifier stands in for the LLM so the tagging path is testable without
// a network call, and so "the classifier misbehaved" is a case with a test
// rather than a hope.
type stubClassifier struct {
	reply    string
	err      error
	calls    int
	sawTemas map[string][]string
}

func (s *stubClassifier) ClassifyArticle(title, content string, domains []string, existing map[string][]string, userID string) (string, error) {
	s.calls++
	s.sawTemas = existing
	return s.reply, s.err
}

func taggedDB(t *testing.T) (*sql.DB, *TagService) {
	t.Helper()
	db := newTestDB(t)
	seedUser(t, db, "u1")
	seedDeck(t, db, "deck", "u1")
	seedArticle(t, db, "art1", "u1", "La armonía en el Barroco", "texto sobre bajo continuo")
	return db, NewTagService(db)
}

// ISC-55 — cards born from an article are tagged at creation, with no manual
// step and without the call site doing anything.
func TestGeneratedCardsAreTaggedFromTheirArticle(t *testing.T) {
	db, tags := taggedDB(t)
	stub := &stubClassifier{reply: "Música/Armonía"}
	cards := NewCardService(db).WithTagging(tags, stub)

	articleID := "art1"
	n, err := cards.CreateBatch("deck", &articleID, []FlashcardPair{
		{Front: "q1", Back: "a1"}, {Front: "q2", Back: "a2"},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if n != 2 {
		t.Fatalf("created %d cards, want 2", n)
	}

	rows, _ := db.Query("SELECT id FROM cards WHERE article_id = 'art1'")
	defer rows.Close()
	for rows.Next() {
		var id string
		rows.Scan(&id)
		got, err := tags.ForCard(id)
		if err != nil {
			t.Fatalf("tags for %s: %v", id, err)
		}
		if len(got) != 1 || got[0].Key != "musica/armonia" {
			t.Errorf("card %s carries %+v, want musica/armonia", id, got)
		}
	}
}

// ISC-55 — a second batch from the same article reuses the tag instead of
// asking again. The daily cron runs against the same articles repeatedly, and
// re-classifying would both cost a call and risk a different answer.
func TestSecondBatchReusesTheArticleTag(t *testing.T) {
	db, tags := taggedDB(t)
	stub := &stubClassifier{reply: "musica/armonia"}
	cards := NewCardService(db).WithTagging(tags, stub)
	articleID := "art1"

	if _, err := cards.CreateBatch("deck", &articleID, []FlashcardPair{{Front: "q1", Back: "a1"}}); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	if _, err := cards.CreateBatch("deck", &articleID, []FlashcardPair{{Front: "q2", Back: "a2"}}); err != nil {
		t.Fatalf("second batch: %v", err)
	}
	if stub.calls != 1 {
		t.Errorf("classifier called %d times for one article, want 1", stub.calls)
	}

	var n int
	db.QueryRow("SELECT COUNT(*) FROM card_tags").Scan(&n)
	if n != 2 {
		t.Errorf("%d cards tagged, want both", n)
	}
}

// ISC-55 — the generator may not invent a first segment. An out-of-list domain
// leaves the card untagged and the cards still get created, because a card that
// cannot be classified is still a card worth having.
func TestAnInventedDomainLeavesTheCardUntaggedRatherThanWrong(t *testing.T) {
	db, tags := taggedDB(t)
	stub := &stubClassifier{reply: "quimica/lavoisier"}
	cards := NewCardService(db).WithTagging(tags, stub)
	articleID := "art1"

	n, err := cards.CreateBatch("deck", &articleID, []FlashcardPair{{Front: "q1", Back: "a1"}})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if n != 1 {
		t.Fatalf("created %d cards, want 1 — a classification failure must not lose the cards", n)
	}
	var tagRows int
	db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&tagRows)
	if tagRows != 0 {
		t.Errorf("%d tag rows created from an out-of-list domain", tagRows)
	}
}

// ISC-55 — the classifier is shown the temas already in use, which is what
// makes it reuse one rather than coin a synonym.
func TestClassifierIsShownTheExistingVocabulary(t *testing.T) {
	db, tags := taggedDB(t)
	if _, err := tags.Ensure("u1", "musica/repertorio"); err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	stub := &stubClassifier{reply: "musica/repertorio"}
	cards := NewCardService(db).WithTagging(tags, stub)
	articleID := "art1"
	if _, err := cards.CreateBatch("deck", &articleID, []FlashcardPair{{Front: "q", Back: "a"}}); err != nil {
		t.Fatalf("create batch: %v", err)
	}

	if got := stub.sawTemas["musica"]; len(got) != 1 || got[0] != "repertorio" {
		t.Errorf("classifier saw %v under musica, want [repertorio]", got)
	}
}

// ISC-56 — the dry run writes nothing. Cards accumulated over months are the
// operator's own work; a bulk pass over them proposes first.
func TestBackfillDryRunWritesNothing(t *testing.T) {
	db, tags := taggedDB(t)
	seedCard(t, db, "c1", "deck", "q", "a")
	mustExec(t, db, "UPDATE cards SET article_id = 'art1' WHERE id = 'c1'")
	stub := &stubClassifier{reply: "musica/armonia"}

	report, err := tags.BackfillTags("u1", stub, false)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if report.Tagged != 1 {
		t.Errorf("dry run proposed %d, want 1", report.Tagged)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM card_tags").Scan(&n)
	if n != 0 {
		t.Errorf("%d rows written by a dry run", n)
	}
}

// ISC-56 — with --apply every card that has a source article gets a tag, and
// cards without one are reported rather than silently skipped.
func TestBackfillTagsAndReportsTheUntaggable(t *testing.T) {
	db, tags := taggedDB(t)
	seedCard(t, db, "withArticle", "deck", "q1", "a1")
	seedCard(t, db, "alsoArticle", "deck", "q2", "a2")
	seedCard(t, db, "handwritten", "deck", "q3", "a3")
	mustExec(t, db, "UPDATE cards SET article_id = 'art1' WHERE id IN ('withArticle','alsoArticle')")
	stub := &stubClassifier{reply: "musica/armonia"}

	report, err := tags.BackfillTags("u1", stub, true)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if report.Tagged != 2 {
		t.Errorf("tagged %d, want 2", report.Tagged)
	}
	if len(report.NoArticle) != 1 || report.NoArticle[0] != "handwritten" {
		t.Errorf("untaggable cards reported as %v, want [handwritten]", report.NoArticle)
	}
	if stub.calls != 1 {
		t.Errorf("classifier called %d times for one article, want 1", stub.calls)
	}
	for _, id := range []string{"withArticle", "alsoArticle"} {
		got, _ := tags.ForCard(id)
		if len(got) != 1 || got[0].Key != "musica/armonia" {
			t.Errorf("%s carries %+v", id, got)
		}
	}

	// A second pass finds nothing new to do.
	again, _ := tags.BackfillTags("u1", stub, true)
	if again.Tagged != 0 || again.AlreadyTagged != 2 {
		t.Errorf("second pass tagged %d / already %d, want 0 / 2", again.Tagged, again.AlreadyTagged)
	}
}

// ISC-57 — a session can be built from a tag, from a lapse floor, or from both,
// across every deck, and it never reaches another user's card.
func TestFilteredSessionSelectsAcrossDecksAndStopsAtTheOwner(t *testing.T) {
	db := buryDB(t)
	tags := NewTagService(db)
	reviews := NewReviewService(db)

	leechSeed(t, db, "armoniaEasy", "deck", 0)
	leechSeed(t, db, "armoniaHard", "deck2", 9)
	leechSeed(t, db, "repertorio", "deck", 11)
	leechSeed(t, db, "theirs", "otherdeck", 20)
	for id, tag := range map[string]string{
		"armoniaEasy": "musica/armonia",
		"armoniaHard": "musica/armonia",
		"repertorio":  "musica/repertorio",
	} {
		if err := tags.Attach("u1", id, tag); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if err := tags.Attach("u2", "theirs", "musica/armonia"); err != nil {
		t.Fatalf("attach theirs: %v", err)
	}

	cases := []struct {
		name  string
		f     StudyFilter
		count int
	}{
		{"by tag, spanning two decks", StudyFilter{TagKey: "musica/armonia"}, 2},
		{"by lapse floor", StudyFilter{MinLapses: 9}, 2},
		{"both together", StudyFilter{TagKey: "musica/armonia", MinLapses: 9}, 1},
		{"a tag nobody owns", StudyFilter{TagKey: "musica/nada"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card, count, err := reviews.GetNextFiltered("u1", tc.f)
			if err != nil {
				t.Fatalf("filtered: %v", err)
			}
			if count != tc.count {
				t.Errorf("count %d, want %d", count, tc.count)
			}
			if tc.count > 0 && card == nil {
				t.Error("count is positive but no card was served")
			}
			if card != nil && card.ID == "theirs" {
				t.Error("served another user's card")
			}
		})
	}

	// The other user's own filtered session sees only their card.
	card, count, _ := reviews.GetNextFiltered("u2", StudyFilter{TagKey: "musica/armonia"})
	if count != 1 || card == nil || card.ID != "theirs" {
		t.Errorf("u2 got count=%d card=%v, want their own single card", count, card)
	}
}

// ISC-57 — a filtered session obeys the same queue rules as a deck session.
// Suspended and buried cards are a selection concern, not a scheduler one, and
// a filter is not a way around them.
func TestFilteredSessionStillHonoursSuspendedAndBuried(t *testing.T) {
	db := buryDB(t)
	tags := NewTagService(db)
	leechSeed(t, db, "live", "deck", 0)
	leechSeed(t, db, "suspended", "deck", 0)
	leechSeed(t, db, "buried", "deck", 0)
	for _, id := range []string{"live", "suspended", "buried"} {
		if err := tags.Attach("u1", id, "musica/armonia"); err != nil {
			t.Fatalf("attach: %v", err)
		}
	}
	mustExec(t, db, "UPDATE cards SET suspended = 1 WHERE id = 'suspended'")
	mustExec(t, db, "UPDATE cards SET buried_until = ? WHERE id = 'buried'",
		time.Now().UTC().Add(6*time.Hour).Format(time.RFC3339))

	card, count, err := NewReviewService(db).GetNextFiltered("u1", StudyFilter{TagKey: "musica/armonia"})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if count != 1 || card == nil || card.ID != "live" {
		t.Fatalf("count=%d card=%v, want only the live card", count, card)
	}
}
