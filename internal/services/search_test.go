package services

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deleyva/recall/internal/database"
	"github.com/deleyva/recall/internal/models"
	"github.com/pressly/goose/v3"
)

// newTestDB builds a database with the real migrations applied, so the tests
// exercise the same schema and triggers that production runs.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(db, "../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func seedUser(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	mustExec(t, db, "INSERT INTO users (id, email, password_hash) VALUES (?, ?, 'x')", id, id+"@example.com")
}

func seedArticle(t *testing.T, db *sql.DB, id, userID, title, content string) {
	t.Helper()
	mustExec(t, db,
		"INSERT INTO articles (id, user_id, url, title, domain, content, created_at) VALUES (?, ?, ?, ?, 'example.com', ?, '2026-01-01T00:00:00Z')",
		id, userID, "https://example.com/"+id, title, content)
}

func seedDeck(t *testing.T, db *sql.DB, id, userID string) {
	t.Helper()
	mustExec(t, db,
		"INSERT INTO decks (id, user_id, name, description, created_at) VALUES (?, ?, 'Deck', '', '2026-01-01T00:00:00Z')",
		id, userID)
}

func seedCard(t *testing.T, db *sql.DB, id, deckID, front, back string) {
	t.Helper()
	mustExec(t, db, `
		INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
			scheduled_days, reps, lapses, state, last_review, created_at, updated_at)
		VALUES (?, ?, ?, ?, '2026-01-01T00:00:00Z', 0, 0, 0, 0, 0, 0, 0,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		id, deckID, front, back)
}

func indexCount(t *testing.T, db *sql.DB, kind, entityID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM search_index WHERE kind = ? AND entity_id = ?", kind, entityID).Scan(&n); err != nil {
		t.Fatalf("count index: %v", err)
	}
	return n
}

// ISC-1/ISC-2 — the migration creates the FTS5 table and backfills every row
// that already existed.
func TestMigrationCreatesAndBackfillsIndex(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "backfill.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	// Migrate up to 012 — the state of the world before search existed.
	if err := goose.UpTo(db, "../../migrations", 12); err != nil {
		t.Fatalf("migrate to 12: %v", err)
	}

	seedUser(t, db, "u1")
	seedArticle(t, db, "a1", "u1", "Historia de la música", "El canto gregoriano nace en la Edad Media.")
	seedDeck(t, db, "d1", "u1")
	seedCard(t, db, "c1", "d1", "¿Qué es el canto gregoriano?", "Canto litúrgico monódico.")
	mustExec(t, db, "INSERT INTO chat_messages (id, article_id, user_id, role, content, created_at) VALUES ('m1','a1','u1','user','¿Cuándo aparece la polifonía?','2026-01-01T00:00:00Z')")

	if err := goose.Up(db, "../../migrations"); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}

	var sqlText string
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE name = 'search_index'").Scan(&sqlText); err != nil {
		t.Fatalf("search_index not created: %v", err)
	}
	if !strings.Contains(strings.ToLower(sqlText), "fts5") {
		t.Fatalf("search_index is not an fts5 table: %s", sqlText)
	}

	for _, tc := range []struct{ kind, id string }{
		{models.SearchKindArticle, "a1"},
		{models.SearchKindFlashcard, "c1"},
		{models.SearchKindChat, "m1"},
	} {
		if n := indexCount(t, db, tc.kind, tc.id); n != 1 {
			t.Errorf("backfill %s/%s: got %d rows, want 1", tc.kind, tc.id, n)
		}
	}
}

// ISC-3/4/5 — triggers keep the index consistent through insert, update, delete.
func TestTriggersKeepIndexInSync(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedDeck(t, db, "d1", "u1")

	seedArticle(t, db, "a1", "u1", "Armonía", "acordes de séptima")
	seedCard(t, db, "c1", "d1", "¿Qué es un acorde?", "Tres o más notas simultáneas")
	mustExec(t, db, "INSERT INTO chat_messages (id, article_id, user_id, role, content, created_at) VALUES ('m1','a1','u1','user','háblame del contrapunto','2026-01-01T00:00:00Z')")

	for _, kind := range []string{models.SearchKindArticle, models.SearchKindFlashcard, models.SearchKindChat} {
		var n int
		db.QueryRow("SELECT COUNT(*) FROM search_index WHERE kind = ?", kind).Scan(&n)
		if n != 1 {
			t.Fatalf("after insert, %s rows = %d, want 1", kind, n)
		}
	}

	// Updates replace rather than duplicate, and the new text is findable.
	mustExec(t, db, "UPDATE articles SET content = 'modulación al relativo mayor' WHERE id = 'a1'")
	if n := indexCount(t, db, models.SearchKindArticle, "a1"); n != 1 {
		t.Errorf("after article update: %d index rows, want 1", n)
	}
	res, _, err := svc.Search("u1", "modulación", nil, 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 || res[0].ID != "a1" {
		t.Errorf("updated article not searchable: %+v", res)
	}
	if res, _, _ := svc.Search("u1", "acordes", nil, 10, 0); len(res) != 0 {
		t.Errorf("stale article text still indexed: %+v", res)
	}

	mustExec(t, db, "UPDATE cards SET back = 'Intervalo de tercera' WHERE id = 'c1'")
	if n := indexCount(t, db, models.SearchKindFlashcard, "c1"); n != 1 {
		t.Errorf("after card update: %d index rows, want 1", n)
	}
	mustExec(t, db, "UPDATE chat_messages SET content = 'explícame el bajo cifrado' WHERE id = 'm1'")
	if n := indexCount(t, db, models.SearchKindChat, "m1"); n != 1 {
		t.Errorf("after chat update: %d index rows, want 1", n)
	}

	mustExec(t, db, "DELETE FROM chat_messages WHERE id = 'm1'")
	mustExec(t, db, "DELETE FROM cards WHERE id = 'c1'")
	mustExec(t, db, "DELETE FROM articles WHERE id = 'a1'")
	var total int
	db.QueryRow("SELECT COUNT(*) FROM search_index").Scan(&total)
	if total != 0 {
		t.Errorf("after deletes, index still holds %d rows", total)
	}
}

// ISC-6 — accents and case do not matter, in either direction.
func TestSearchIgnoresAccentsAndCase(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedArticle(t, db, "a1", "u1", "Música clásica", "La notación mensural surgió en el siglo XIII.")

	for _, q := range []string{"musica", "MÚSICA", "Música", "notacion", "NOTACIÓN"} {
		res, _, err := svc.Search("u1", q, nil, 10, 0)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if len(res) != 1 {
			t.Errorf("query %q returned %d results, want 1", q, len(res))
		}
	}
}

// ISC-7 — no cross-user leakage.
func TestSearchIsScopedToUser(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedUser(t, db, "u2")
	seedArticle(t, db, "a1", "u1", "Secreto de u1", "contrapunto floreado")
	seedDeck(t, db, "d2", "u2")
	seedCard(t, db, "c2", "d2", "contrapunto", "de u2")

	res, total, err := svc.Search("u1", "contrapunto", nil, 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(res) != 1 || res[0].ID != "a1" {
		t.Fatalf("u1 search leaked rows: total=%d results=%+v", total, res)
	}

	res2, _, _ := svc.Search("u2", "contrapunto", nil, 10, 0)
	if len(res2) != 1 || res2[0].ID != "c2" {
		t.Fatalf("u2 search wrong: %+v", res2)
	}
}

// ISC-8 — hostile input never produces an FTS5 syntax error.
func TestSearchNeverErrorsOnHostileInput(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedArticle(t, db, "a1", "u1", "Test", "el canto gregoriano y la polifonía")

	inputs := []string{
		"", "   ", "\t\n", `"`, `""`, `"unclosed`, "AND", "OR", "NOT", "and or not",
		"*", "**", "^", "^abc", "(", ")", "()", "( AND )", "-", "--", "- -",
		"NEAR(a b)", "a:b", "canto*", "{}", "[]", "\\", "%", "'", "''",
		"🎸🎼", "café", "ñ", strings.Repeat("a", 500), strings.Repeat("canto ", 100),
		"canto AND NOT gregoriano", "\"canto\" OR (x)", "1+1", "..", "…",
	}
	for _, in := range inputs {
		if _, _, err := svc.Search("u1", in, nil, 10, 0); err != nil {
			t.Errorf("query %q returned error: %v", in, err)
		}
	}
}

// ISC-9 — snippets mark the match and escape stored HTML instead of rendering it.
func TestSnippetMarksMatchAndEscapesHTML(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedDeck(t, db, "d1", "u1")
	seedCard(t, db, "c1", "d1", "<b>¿Qué es la fuga?</b>", "<ul><li>Forma <i>contrapuntística</i> basada en imitación</li></ul>")

	res, _, err := svc.Search("u1", "contrapuntistica", nil, 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	snip := res[0].Snippet
	if !strings.Contains(snip, "<mark>contrapuntística</mark>") {
		t.Errorf("match not marked: %q", snip)
	}
	if strings.Contains(snip, "<li>") || strings.Contains(snip, "<i>") {
		t.Errorf("stored HTML survived into the snippet: %q", snip)
	}
	if strings.Contains(res[0].Title, "<b>") {
		t.Errorf("stored HTML survived into the title: %q", res[0].Title)
	}
}

// ISC-10 — better matches rank first.
func TestSearchRanksByRelevance(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedArticle(t, db, "weak", "u1", "Historia general", strings.Repeat("relleno sin interés. ", 200)+" una mención de fuga al final")
	seedArticle(t, db, "strong", "u1", "La fuga", "fuga, fuga y más fuga: la fuga como forma")

	res, _, err := svc.Search("u1", "fuga", nil, 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 results, got %d", len(res))
	}
	if res[0].ID != "strong" {
		t.Errorf("ranking wrong: got %s first (scores %v, %v)", res[0].ID, res[0].Score, res[1].Score)
	}
}

func TestSearchKindFilterAndResultLinks(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedDeck(t, db, "d1", "u1")
	seedArticle(t, db, "a1", "u1", "Ritmo", "el compás de amalgama")
	seedCard(t, db, "c1", "d1", "compás", "unidad métrica")

	all, total, _ := svc.Search("u1", "compás", nil, 10, 0)
	if total != 2 || len(all) != 2 {
		t.Fatalf("want 2 results across kinds, got %d", len(all))
	}

	only, total, _ := svc.Search("u1", "compás", []string{models.SearchKindArticle}, 10, 0)
	if total != 1 || len(only) != 1 || only[0].Kind != models.SearchKindArticle {
		t.Fatalf("kind filter failed: %+v", only)
	}
	if only[0].URL != "/to-read/a1/read?q=comp%C3%A1s" {
		t.Errorf("article link wrong: %s", only[0].URL)
	}

	cards, _, _ := svc.Search("u1", "compás", []string{models.SearchKindFlashcard}, 10, 0)
	if len(cards) != 1 || cards[0].URL != "/decks/d1/cards/c1/edit" {
		t.Errorf("flashcard link wrong: %+v", cards)
	}

	// An unknown kind is ignored rather than silently returning nothing.
	any, _, _ := svc.Search("u1", "compás", []string{"nonsense"}, 10, 0)
	if len(any) != 2 {
		t.Errorf("unknown kind filter changed results: %d", len(any))
	}
}

func TestReindexRebuildsFromSourceTables(t *testing.T) {
	db := newTestDB(t)
	svc := NewSearchService(db)
	seedUser(t, db, "u1")
	seedArticle(t, db, "a1", "u1", "Timbre", "la orquestación de Ravel")

	mustExec(t, db, "DELETE FROM search_index")
	if res, _, _ := svc.Search("u1", "orquestación", nil, 10, 0); len(res) != 0 {
		t.Fatalf("index should be empty after manual wipe")
	}

	n, err := svc.Reindex()
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if n != 1 {
		t.Errorf("reindex counted %d rows, want 1", n)
	}
	if res, _, _ := svc.Search("u1", "orquestación", nil, 10, 0); len(res) != 1 {
		t.Errorf("reindex did not restore the row")
	}
}

func TestMatchExpr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"canto", `"canto"*`},
		{"canto gregoriano", `"canto" "gregoriano"*`},
	}
	for _, tc := range cases {
		if got := matchExpr(Tokens(tc.in)); got != tc.want {
			t.Errorf("matchExpr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
