package services

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

// The fixture is small enough that every expected number below is worked out by
// hand in the comments, so a failing assertion points at the metric rather than
// at the fixture.
func seedMetricsFixture(t *testing.T, db *sql.DB) {
	t.Helper()

	seedUser(t, db, "u1")
	seedUser(t, db, "u2")

	mustExec(t, db, "INSERT INTO decks (id, user_id, name, description, created_at) VALUES ('dA','u1','Alpha','','2026-01-01T00:00:00Z')")
	mustExec(t, db, "INSERT INTO decks (id, user_id, name, description, created_at) VALUES ('dB','u1','Beta','','2026-01-01T00:00:00Z')")
	mustExec(t, db, "INSERT INTO decks (id, user_id, name, description, created_at) VALUES ('dX','u2','Other','','2026-01-01T00:00:00Z')")

	seedArticle(t, db, "a1", "u1", "Article one", "body one")
	seedArticle(t, db, "a2", "u1", "Article two", "body two")

	// c1, c2, c5 belong to article a1; c3 to a2; c4 to none.
	seedCard(t, db, "c1", "dA", "¿Qué es X?", "Uno")
	seedCard(t, db, "c2", "dA", "¿Qué es Y y Z?", "<ul><li>a</li><li>b</li></ul>")
	seedCard(t, db, "c3", "dB", "¿Quién fue W?", "Dos")
	seedCard(t, db, "c4", "dB", "What is A and B?", "Tres")
	seedCard(t, db, "c5", "dA", "Simple", "Cuatro")
	mustExec(t, db, "UPDATE cards SET article_id='a1' WHERE id IN ('c1','c2','c5')")
	mustExec(t, db, "UPDATE cards SET article_id='a2' WHERE id='c3'")
	mustExec(t, db, "UPDATE cards SET lapses=9 WHERE id='c3'")

	// Another user's card is a leech, has a list back and a compound front. None
	// of it may appear in u1's numbers.
	seedCard(t, db, "cx", "dX", "¿P y Q?", "<ul><li>z</li></ul>")
	mustExec(t, db, "UPDATE cards SET lapses=20 WHERE id='cx'")

	rev := func(id, cardID string, rating, elapsed int, at string) {
		mustExec(t, db,
			"INSERT INTO review_logs (id, card_id, rating, scheduled_days, elapsed_days, review_time, state) VALUES (?,?,?,0,?,?,2)",
			id, cardID, rating, elapsed, at)
	}
	rev("r1", "c1", 3, 0, "2026-02-01T10:00:00Z")
	rev("r2", "c2", 2, 3, "2026-02-01T11:00:00Z")
	rev("r3", "c3", 4, 7, "2026-02-01T12:00:00Z")
	rev("r4", "c5", 3, 0, "2026-02-06T09:00:00Z")
	rev("r5", "c1", 3, 5, "2026-02-06T10:00:00Z")
	rev("r6", "c2", 1, 4, "2026-02-10T11:00:00Z")
	rev("r7", "c1", 1, 10, "2026-02-16T10:00:00Z")
	rev("rx", "cx", 1, 9, "2026-02-16T23:00:00Z")
}

func computeFixture(t *testing.T) *Metrics {
	t.Helper()
	db := newTestDB(t)
	seedMetricsFixture(t, db)
	m, err := NewMetricsService(db).Compute("u1@example.com", time.UTC)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	return m
}

// ISC-26 — corpus counts are scoped to the requested user.
func TestMetricsCorpusIsUserScoped(t *testing.T) {
	m := computeFixture(t)

	if m.User != "u1@example.com" {
		t.Errorf("user = %q, want u1@example.com", m.User)
	}
	if m.Corpus.Decks != 2 {
		t.Errorf("decks = %d, want 2", m.Corpus.Decks)
	}
	if m.Corpus.Cards != 5 {
		t.Errorf("cards = %d, want 5 (u2's card must not count)", m.Corpus.Cards)
	}
	if m.Corpus.Articles != 2 {
		t.Errorf("articles = %d, want 2", m.Corpus.Articles)
	}
	if m.Corpus.Reviews != 7 {
		t.Errorf("reviews = %d, want 7 (u2's review must not count)", m.Corpus.Reviews)
	}
	if m.Corpus.ActiveDays != 4 {
		t.Errorf("active days = %d, want 4", m.Corpus.ActiveDays)
	}
	if m.Corpus.FirstReview != "2026-02-01" || m.Corpus.LastReview != "2026-02-16" {
		t.Errorf("range = %s..%s, want 2026-02-01..2026-02-16", m.Corpus.FirstReview, m.Corpus.LastReview)
	}
}

// ISC-26 — true retention counts only spaced reviews (elapsed_days > 0).
// Spaced: r2(2), r3(4), r5(3), r6(1), r7(1) = 5 reviews, 2 of them Again → 60.0%.
func TestMetricsTrueRetentionCountsOnlySpacedReviews(t *testing.T) {
	m := computeFixture(t)

	if m.TrueRetention.SpacedReviews != 5 {
		t.Errorf("spaced reviews = %d, want 5", m.TrueRetention.SpacedReviews)
	}
	if m.TrueRetention.Again != 2 {
		t.Errorf("again = %d, want 2", m.TrueRetention.Again)
	}
	if m.TrueRetention.RetentionPct != 60.0 {
		t.Errorf("retention = %.1f, want 60.0", m.TrueRetention.RetentionPct)
	}
}

// ISC-26 — the four-way rating distribution over every review.
// r1..r7: again 2, hard 1, good 3, easy 1.
func TestMetricsRatingDistribution(t *testing.T) {
	m := computeFixture(t)

	if len(m.Ratings) != 4 {
		t.Fatalf("ratings rows = %d, want 4", len(m.Ratings))
	}
	want := []struct {
		rating int
		n      int
		pct    float64
	}{
		{1, 2, 28.6},
		{2, 1, 14.3},
		{3, 3, 42.9},
		{4, 1, 14.3},
	}
	for i, w := range want {
		got := m.Ratings[i]
		if got.Rating != w.rating || got.Count != w.n || got.Pct != w.pct {
			t.Errorf("ratings[%d] = {%d %d %.1f}, want {%d %d %.1f}",
				i, got.Rating, got.Count, got.Pct, w.rating, w.n, w.pct)
		}
	}
}

// ISC-62's probe — whether a rating predicts a subsequent Again.
// c1: 3→3, 3→1 (so Good has 2 followups, 1 Again = 50.0%)
// c2: 2→1     (so Hard has 1 followup, 1 Again = 100.0%)
func TestMetricsRatingFollowupPredictsNextAgain(t *testing.T) {
	m := computeFixture(t)

	if len(m.RatingFollowup) != 4 {
		t.Fatalf("followup rows = %d, want 4", len(m.RatingFollowup))
	}
	byRating := map[int]RatingFollowup{}
	for _, r := range m.RatingFollowup {
		byRating[r.Rating] = r
	}
	if got := byRating[2]; got.Followups != 1 || got.NextAgainPct != 100.0 {
		t.Errorf("hard followup = {%d %.1f}, want {1 100.0}", got.Followups, got.NextAgainPct)
	}
	if got := byRating[3]; got.Followups != 2 || got.NextAgainPct != 50.0 {
		t.Errorf("good followup = {%d %.1f}, want {2 50.0}", got.Followups, got.NextAgainPct)
	}
	if got := byRating[1]; got.Followups != 0 || got.NextAgainPct != 0.0 {
		t.Errorf("again followup = {%d %.1f}, want {0 0.0}", got.Followups, got.NextAgainPct)
	}
}

// ISC-26 — leeches at or above the threshold, the other user's excluded.
func TestMetricsLeeches(t *testing.T) {
	m := computeFixture(t)

	if m.Leeches.Threshold != LeechThreshold {
		t.Errorf("threshold = %d, want %d", m.Leeches.Threshold, LeechThreshold)
	}
	if m.Leeches.Count != 1 {
		t.Errorf("leeches = %d, want 1 (u2's 20-lapse card must not count)", m.Leeches.Count)
	}
	if m.Leeches.MaxLapses != 9 {
		t.Errorf("max lapses = %d, want 9", m.Leeches.MaxLapses)
	}
}

// ISC-39's probe — share of reviews landing on the same day as a sibling of the
// same article. Groups: (02-01,a1)=2 ✓, (02-01,a2)=1, (02-06,a1)=2 ✓,
// (02-10,a1)=1, (02-16,a1)=1 → 4 of 7 article-linked reviews = 57.1%, max 2.
func TestMetricsSiblingSameDayShare(t *testing.T) {
	m := computeFixture(t)

	if m.SiblingSameDay.ArticleReviews != 7 {
		t.Errorf("article-linked reviews = %d, want 7", m.SiblingSameDay.ArticleReviews)
	}
	if m.SiblingSameDay.WithSibling != 4 {
		t.Errorf("with sibling = %d, want 4", m.SiblingSameDay.WithSibling)
	}
	if m.SiblingSameDay.Pct != 57.1 {
		t.Errorf("sibling share = %.1f, want 57.1", m.SiblingSameDay.Pct)
	}
	if m.SiblingSameDay.MaxPerDay != 2 {
		t.Errorf("max per day = %d, want 2", m.SiblingSameDay.MaxPerDay)
	}
}

// ISC-55's probe — formulation markers. One of five backs carries a list; two of
// five fronts carry a coordinating conjunction ("y" in c2, "and" in c4).
func TestMetricsFormulationMarkers(t *testing.T) {
	m := computeFixture(t)

	if m.ListBacks.Count != 1 || m.ListBacks.Total != 5 || m.ListBacks.Pct != 20.0 {
		t.Errorf("list backs = {%d/%d %.1f}, want {1/5 20.0}", m.ListBacks.Count, m.ListBacks.Total, m.ListBacks.Pct)
	}
	if m.CompoundFronts.Count != 2 || m.CompoundFronts.Total != 5 || m.CompoundFronts.Pct != 40.0 {
		t.Errorf("compound fronts = {%d/%d %.1f}, want {2/5 40.0}", m.CompoundFronts.Count, m.CompoundFronts.Total, m.CompoundFronts.Pct)
	}
}

// ISC-26 — cards per deck, ordered by count descending then name ascending.
func TestMetricsDeckDistributionIsOrdered(t *testing.T) {
	m := computeFixture(t)

	if len(m.Decks) != 2 {
		t.Fatalf("decks = %d, want 2", len(m.Decks))
	}
	if m.Decks[0].Name != "Alpha" || m.Decks[0].Cards != 3 {
		t.Errorf("decks[0] = {%s %d}, want {Alpha 3}", m.Decks[0].Name, m.Decks[0].Cards)
	}
	if m.Decks[1].Name != "Beta" || m.Decks[1].Cards != 2 {
		t.Errorf("decks[1] = {%s %d}, want {Beta 2}", m.Decks[1].Name, m.Decks[1].Cards)
	}
}

// ISC-26 — reviews per local hour, in the requested zone. In UTC the fixture
// puts 1 review at 09h, 3 at 10h, 2 at 11h and 1 at 12h.
func TestMetricsHourHistogramUsesRequestedZone(t *testing.T) {
	m := computeFixture(t)

	if len(m.Hours) != 24 {
		t.Fatalf("hour rows = %d, want 24", len(m.Hours))
	}
	want := map[int]int{9: 1, 10: 3, 11: 2, 12: 1}
	for h, row := range m.Hours {
		if row.Hour != h {
			t.Fatalf("hours[%d].Hour = %d, want %d (slice must be ordered 0..23)", h, row.Hour, h)
		}
		if row.Reviews != want[h] {
			t.Errorf("hour %02d = %d reviews, want %d", h, row.Reviews, want[h])
		}
	}
	if m.Timezone != "UTC" {
		t.Errorf("timezone = %q, want UTC", m.Timezone)
	}
}

// The same fixture read in a zone one hour ahead shifts every review by an hour,
// which proves the histogram is computed in the requested zone rather than in UTC.
func TestMetricsHourHistogramShiftsWithZone(t *testing.T) {
	db := newTestDB(t)
	seedMetricsFixture(t, db)

	loc := time.FixedZone("plus-one", 3600)
	m, err := NewMetricsService(db).Compute("u1@example.com", loc)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.Hours[11].Reviews != 3 {
		t.Errorf("hour 11 = %d reviews, want 3 (10h UTC shifted by one hour)", m.Hours[11].Reviews)
	}
	if m.Hours[10].Reviews != 1 {
		t.Errorf("hour 10 = %d reviews, want 1", m.Hours[10].Reviews)
	}
}

// ISC-27 — the rendered report is byte-identical across runs against an
// unchanged database. Anything non-deterministic (map iteration, a timestamp in
// the header) breaks this.
func TestMetricsRenderIsByteIdenticalAcrossRuns(t *testing.T) {
	db := newTestDB(t)
	seedMetricsFixture(t, db)
	svc := NewMetricsService(db)

	render := func() []byte {
		m, err := svc.Compute("u1@example.com", time.UTC)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		var buf bytes.Buffer
		if err := RenderMetrics(&buf, m); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.Bytes()
	}

	first, second := render(), render()
	if !bytes.Equal(first, second) {
		t.Errorf("report differs between runs:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if len(first) == 0 {
		t.Error("report is empty")
	}
}

// ISC-26 — the JSON form is stable too, so a probe can diff it directly.
func TestMetricsJSONIsByteIdenticalAcrossRuns(t *testing.T) {
	db := newTestDB(t)
	seedMetricsFixture(t, db)
	svc := NewMetricsService(db)

	marshal := func() []byte {
		m, err := svc.Compute("u1@example.com", time.UTC)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return b
	}

	if first, second := marshal(), marshal(); !bytes.Equal(first, second) {
		t.Errorf("json differs between runs:\n%s\n%s", first, second)
	}
}

// An unknown user is an error, not an empty report that reads like a real zero.
func TestMetricsUnknownUserIsAnError(t *testing.T) {
	db := newTestDB(t)
	seedMetricsFixture(t, db)

	if _, err := NewMetricsService(db).Compute("nobody@example.com", time.UTC); err == nil {
		t.Error("expected an error for an unknown user, got nil")
	}
}

// A user with no reviews yet must not divide by zero anywhere.
func TestMetricsEmptyCorpusDoesNotPanic(t *testing.T) {
	db := newTestDB(t)
	seedUser(t, db, "empty")

	m, err := NewMetricsService(db).Compute("empty@example.com", time.UTC)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.TrueRetention.RetentionPct != 0 || m.ListBacks.Pct != 0 || m.SiblingSameDay.Pct != 0 {
		t.Errorf("empty corpus should report zeroes, got %+v", m)
	}
	var buf bytes.Buffer
	if err := RenderMetrics(&buf, m); err != nil {
		t.Fatalf("render empty: %v", err)
	}
}
