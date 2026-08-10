package services

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/deleyva/recall/internal/models"
)

// SearchService queries the FTS5 index that migration 013 keeps in sync with
// articles, cards and chat messages.
type SearchService struct {
	db *sql.DB
}

func NewSearchService(db *sql.DB) *SearchService {
	return &SearchService{db: db}
}

// Valid values for the kind filter.
var searchKinds = map[string]bool{
	models.SearchKindArticle:   true,
	models.SearchKindFlashcard: true,
	models.SearchKindChat:      true,
}

const snippetWidth = 240

// matchExpr turns folded terms into an FTS5 MATCH expression. Terms are always
// quoted, so operator words the user did not mean as syntax ("and", "not") and
// anything else that survived tokenisation cannot become a query error. The
// last term gets a prefix wildcard so search-as-you-type finds partial words.
func matchExpr(terms []string) string {
	parts := make([]string, 0, len(terms))
	for i, t := range terms {
		q := `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
		if i == len(terms)-1 {
			q += "*"
		}
		parts = append(parts, q)
	}
	return strings.Join(parts, " ")
}

// Search runs a full-text query scoped to one user. An empty or symbol-only
// query returns no results and no error. kinds filters by result type; nil or
// empty means all kinds.
func (s *SearchService) Search(userID, query string, kinds []string, limit, offset int) ([]models.SearchResult, int, error) {
	terms := Tokens(query)
	if len(terms) == 0 {
		return []models.SearchResult{}, 0, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	where := "search_index MATCH ? AND user_id = ?"
	args := []interface{}{matchExpr(terms), userID}

	if filter := validKinds(kinds); len(filter) > 0 {
		where += " AND kind IN (" + placeholders(len(filter)) + ")"
		for _, k := range filter {
			args = append(args, k)
		}
	}

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM search_index WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count search results: %w", err)
	}

	// Title is weighted above body so a hit in an article title or a flashcard
	// question outranks a passing mention deep in a body.
	rows, err := s.db.Query(`
		SELECT kind, entity_id, parent_id, deck_id, title, body,
			bm25(search_index, 5.0, 1.0, 0, 0, 0, 0, 0) AS score
		FROM search_index
		WHERE `+where+`
		ORDER BY score
		LIMIT ? OFFSET ?
	`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	results := []models.SearchResult{}
	for rows.Next() {
		var r models.SearchResult
		var title, body string
		if err := rows.Scan(&r.Kind, &r.ID, &r.ArticleID, &r.DeckID, &title, &body, &r.Score); err != nil {
			return nil, 0, fmt.Errorf("scan search result: %w", err)
		}

		plainBody := StripHTML(body)
		r.Title = strings.TrimSpace(StripHTML(title))
		if len(Matches(plainBody, terms)) == 0 && len(Matches(r.Title, terms)) > 0 {
			// The hit was in the title — highlight that rather than showing an
			// arbitrary head-of-body window with nothing marked in it.
			r.Snippet = Snippet(title, terms, snippetWidth)
		} else {
			r.Snippet = Snippet(plainBody, terms, snippetWidth)
		}
		r.URL = resultURL(r, query)
		results = append(results, r)
	}
	return results, total, rows.Err()
}

func resultURL(r models.SearchResult, query string) string {
	q := "?q=" + url.QueryEscape(query)
	switch r.Kind {
	case models.SearchKindArticle:
		return "/to-read/" + r.ID + "/read" + q
	case models.SearchKindChat:
		if r.ArticleID != "" {
			return "/to-read/" + r.ArticleID + "/chat"
		}
	case models.SearchKindFlashcard:
		if r.DeckID != "" {
			return "/decks/" + r.DeckID + "/cards/" + r.ID + "/edit"
		}
	}
	return ""
}

// Reindex rebuilds the whole index from the source tables. The triggers keep it
// current in normal operation; this is the repair path (`recall reindex`).
func (s *SearchService) Reindex() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin reindex: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM search_index"); err != nil {
		return 0, fmt.Errorf("clear index: %w", err)
	}
	stmts := []string{
		`INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
		 SELECT a.title, a.content, 'article', a.id, a.user_id, a.id, '' FROM articles a`,
		`INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
		 SELECT c.front, c.back, 'flashcard', c.id,
		        (SELECT d.user_id FROM decks d WHERE d.id = c.deck_id),
		        COALESCE(c.article_id, ''), c.deck_id FROM cards c`,
		`INSERT INTO search_index (title, body, kind, entity_id, user_id, parent_id, deck_id)
		 SELECT COALESCE((SELECT a.title FROM articles a WHERE a.id = m.article_id), ''),
		        m.content, 'chat', m.id, m.user_id, m.article_id, '' FROM chat_messages m`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return 0, fmt.Errorf("reindex: %w", err)
		}
	}

	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM search_index").Scan(&count); err != nil {
		return 0, fmt.Errorf("count index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit reindex: %w", err)
	}
	return count, nil
}

func validKinds(kinds []string) []string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		k = strings.TrimSpace(strings.ToLower(k))
		if searchKinds[k] {
			out = append(out, k)
		}
	}
	return out
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
