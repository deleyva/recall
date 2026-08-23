package services

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

// TagDomains is the closed first segment of every tag. It is short and it does
// not grow by accident: a vocabulary whose root drifts is a vocabulary nobody
// can navigate, and the friction of changing this list is the mechanism that
// keeps it stable. Derived from TELOS; the reasoning lives in the LifeOS
// tagging standard.
var TagDomains = []string{
	"musica",
	"humanidades",
	"educacion",
	"sistemas",
	"conocimiento",
	"dinero",
	"vida",
}

// TagDepth is fixed at two segments. One is too coarse to filter on; three or
// more reopens the argument about where each level ends, and both answers are
// always defensible.
const TagDepth = 2

// TagSeparator is canonical. Anki-family surfaces render `::`; that is a
// display concern converted at the boundary, never stored.
const TagSeparator = "/"

func IsTagDomain(s string) bool {
	for _, d := range TagDomains {
		if d == s {
			return true
		}
	}
	return false
}

// NormalizeTagKey reduces a tag to the form two tags are compared on: folded to
// lowercase without diacritics, every run of non-alphanumerics collapsed to a
// single hyphen, segments preserved. It reuses the same fold the search index
// uses, so "the same string" means one thing across the application.
//
// `Música / Teoría Armónica` and `musica/teoria-armonica` normalize to the same
// key, which is to say they are the same tag.
func NormalizeTagKey(raw string) string {
	segments := strings.Split(raw, TagSeparator)
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		folded := Fold(strings.TrimSpace(seg))
		var b strings.Builder
		lastHyphen := true // trims leading hyphens
		for _, r := range folded {
			if isAlnum(r) {
				b.WriteRune(r)
				lastHyphen = false
				continue
			}
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
		out = append(out, strings.TrimRight(b.String(), "-"))
	}
	return strings.Join(out, TagSeparator)
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// ValidateTag returns the normalized key and its domain, or an error naming
// what is wrong. Callers may not invent a domain: an unknown first segment is
// refused rather than created, which is what stops a generator from growing the
// root one article at a time.
func ValidateTag(raw string) (key, domain string, err error) {
	key = NormalizeTagKey(raw)
	segments := strings.Split(key, TagSeparator)
	if len(segments) != TagDepth {
		return "", "", fmt.Errorf("tag %q must be dominio%stema, exactly %d segments", raw, TagSeparator, TagDepth)
	}
	if segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("tag %q has an empty segment", raw)
	}
	if !IsTagDomain(segments[0]) {
		return "", "", fmt.Errorf("tag %q: %q is not one of the closed domains %v", raw, segments[0], TagDomains)
	}
	return key, segments[0], nil
}

type TagService struct {
	db *sql.DB
}

func NewTagService(db *sql.DB) *TagService {
	return &TagService{db: db}
}

// Ensure returns the id of the user's tag with this key, creating it if it does
// not exist. Two spellings of one tag converge here rather than accumulating,
// because the key is what the unique index is on.
func (s *TagService) Ensure(userID, raw string) (string, error) {
	return ensureTag(s.db, userID, raw)
}

type execer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

func ensureTag(db execer, userID, raw string) (string, error) {
	key, domain, err := ValidateTag(raw)
	if err != nil {
		return "", err
	}

	var id string
	err = db.QueryRow("SELECT id FROM tags WHERE user_id = ? AND key = ?", userID, key).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("lookup tag: %w", err)
	}

	id = generateID()
	if _, err := db.Exec(
		"INSERT INTO tags (id, user_id, key, display, domain, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, userID, key, strings.TrimSpace(raw), domain, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return "", fmt.Errorf("create tag: %w", err)
	}
	return id, nil
}

// Attach links a card to a tag, creating the tag if needed. Re-attaching is a
// no-op rather than an error.
func (s *TagService) Attach(userID, cardID, raw string) error {
	return attachTag(s.db, userID, cardID, raw)
}

func attachTag(db execer, userID, cardID, raw string) error {
	tagID, err := ensureTag(db, userID, raw)
	if err != nil {
		return err
	}
	if _, err := db.Exec(
		"INSERT OR IGNORE INTO card_tags (card_id, tag_id) VALUES (?, ?)", cardID, tagID,
	); err != nil {
		return fmt.Errorf("attach tag: %w", err)
	}
	return nil
}

// ForCard returns a card's tags in display form.
func (s *TagService) ForCard(cardID string) ([]models.Tag, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.key, t.display, t.domain
		FROM tags t JOIN card_tags ct ON ct.tag_id = t.id
		WHERE ct.card_id = ?
		ORDER BY t.key
	`, cardID)
	if err != nil {
		return nil, fmt.Errorf("tags for card: %w", err)
	}
	defer rows.Close()
	return scanTags(rows)
}

// ListForUser returns every tag the user owns with the number of cards on it,
// worst-populated last. This is the list a tag input offers, which is what
// makes reuse cheaper than invention.
func (s *TagService) ListForUser(userID string) ([]models.Tag, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.key, t.display, t.domain, COUNT(ct.card_id)
		FROM tags t LEFT JOIN card_tags ct ON ct.tag_id = t.id
		WHERE t.user_id = ?
		GROUP BY t.id
		ORDER BY t.key
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()

	tags := []models.Tag{}
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Key, &t.Display, &t.Domain, &t.CardCount); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// TemasIn returns the existing second segments under one domain. The classifier
// is shown this list so it reuses a tema rather than coining a synonym — the
// machine equivalent of an autocomplete that offers what already exists.
func (s *TagService) TemasIn(userID, domain string) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT key FROM tags WHERE user_id = ? AND domain = ? ORDER BY key", userID, domain)
	if err != nil {
		return nil, fmt.Errorf("temas in domain: %w", err)
	}
	defer rows.Close()

	var temas []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if _, tema, ok := strings.Cut(key, TagSeparator); ok {
			temas = append(temas, tema)
		}
	}
	return temas, rows.Err()
}

func scanTags(rows *sql.Rows) ([]models.Tag, error) {
	tags := []models.Tag{}
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Key, &t.Display, &t.Domain); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ArticleClassifier proposes one `dominio/tema` for an article. It is an
// interface so the tagging path can be tested without a network call, and so a
// failing classifier degrades to "no tag" rather than to a bad tag.
type ArticleClassifier interface {
	ClassifyArticle(title, content string, domains []string, existing map[string][]string, userID string) (string, error)
}

// TagForArticle resolves the tag every card generated from one article should
// carry. It asks the classifier only when it has to: if a sibling card is
// already tagged, that tag is reused, so a batch is consistent and the daily
// cron does not re-classify the same article every run.
//
// Returns an empty string when no valid tag could be resolved. That is a
// reported outcome, never an invented tag: the closed root is only closed if
// failure means "none" rather than "make one up".
func (s *TagService) TagForArticle(userID, articleID string, classifier ArticleClassifier) (string, error) {
	var existingKey string
	err := s.db.QueryRow(`
		SELECT t.key FROM tags t
		JOIN card_tags ct ON ct.tag_id = t.id
		JOIN cards c ON c.id = ct.card_id
		WHERE c.article_id = ? AND t.user_id = ?
		LIMIT 1
	`, articleID, userID).Scan(&existingKey)
	if err == nil {
		return existingKey, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("look up article tag: %w", err)
	}
	if classifier == nil {
		return "", nil
	}

	var title, content string
	if err := s.db.QueryRow(
		"SELECT title, content FROM articles WHERE id = ? AND user_id = ?", articleID, userID,
	).Scan(&title, &content); err != nil {
		return "", fmt.Errorf("read article: %w", err)
	}

	existing := map[string][]string{}
	for _, d := range TagDomains {
		temas, err := s.TemasIn(userID, d)
		if err != nil {
			return "", err
		}
		if len(temas) > 0 {
			existing[d] = temas
		}
	}

	proposed, err := classifier.ClassifyArticle(title, content, TagDomains, existing, userID)
	if err != nil {
		return "", fmt.Errorf("classify article: %w", err)
	}
	key, _, err := ValidateTag(proposed)
	if err != nil {
		return "", fmt.Errorf("classifier proposed an invalid tag: %w", err)
	}
	return key, nil
}

// BackfillReport is what a backfill pass found. Untaggable is the part that
// matters: cards with no source article cannot be tagged from one, and a
// backfill that silently skipped them would report success over a collection
// half of which has no tags.
type BackfillReport struct {
	AlreadyTagged int
	Tagged        int
	ByTag         map[string]int
	NoArticle     []string
	Failed        map[string]string
}

// BackfillTags gives every card with a source article at least one tag derived
// from it. It writes nothing unless apply is true: cards accumulated over months
// are the operator's own work, and a bulk write over them proposes first.
func (s *TagService) BackfillTags(userID string, classifier ArticleClassifier, apply bool) (*BackfillReport, error) {
	report := &BackfillReport{ByTag: map[string]int{}, Failed: map[string]string{}}

	rows, err := s.db.Query(`
		SELECT c.id, c.article_id, EXISTS(SELECT 1 FROM card_tags ct WHERE ct.card_id = c.id)
		FROM cards c JOIN decks d ON d.id = c.deck_id
		WHERE d.user_id = ?
		ORDER BY c.article_id, c.created_at
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("scan cards: %w", err)
	}
	defer rows.Close()

	type pending struct{ cardID, articleID string }
	var todo []pending
	for rows.Next() {
		var cardID string
		var articleID *string
		var tagged bool
		if err := rows.Scan(&cardID, &articleID, &tagged); err != nil {
			return nil, fmt.Errorf("scan card: %w", err)
		}
		switch {
		case tagged:
			report.AlreadyTagged++
		case articleID == nil || *articleID == "":
			report.NoArticle = append(report.NoArticle, cardID)
		default:
			todo = append(todo, pending{cardID, *articleID})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// One classification per article, not per card.
	resolved := map[string]string{}
	for _, p := range todo {
		tag, seen := resolved[p.articleID]
		if !seen {
			tag, err = s.TagForArticle(userID, p.articleID, classifier)
			if err != nil {
				report.Failed[p.articleID] = err.Error()
				tag = ""
			}
			resolved[p.articleID] = tag
		}
		if tag == "" {
			continue
		}
		if apply {
			if err := s.Attach(userID, p.cardID, tag); err != nil {
				report.Failed[p.cardID] = err.Error()
				continue
			}
		}
		report.Tagged++
		report.ByTag[tag]++
	}
	return report, nil
}
