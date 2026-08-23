package services

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/deleyva/recall/internal/models"
)

// CardSplitter proposes atomic replacements for a card that asks too much at
// once. An interface so the splitter is testable without a network call, and so
// a failing proposer degrades to "no proposal" rather than to a bad rewrite.
type CardSplitter interface {
	SplitCard(front, back, userID string) ([]FlashcardPair, error)
}

// SplitCandidate is one malformed card and what would replace it.
type SplitCandidate struct {
	Card     models.Card
	Reason   string
	Proposed []FlashcardPair
	Err      string
}

type SplitService struct {
	db *sql.DB
}

func NewSplitService(db *sql.DB) *SplitService {
	return &SplitService{db: db}
}

// Candidates finds every card of the user's whose back carries list markup or
// whose front carries a coordinating conjunction. It uses the same two
// detectors `recall metrics` measures with, so the thing being fixed and the
// thing being counted are the same thing by construction rather than by
// agreement between two implementations.
func (s *SplitService) Candidates(userID string) ([]SplitCandidate, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.deck_id, c.front, c.back, c.due, c.stability, c.difficulty, c.elapsed_days,
			c.scheduled_days, c.reps, c.lapses, c.state, c.last_review, c.created_at, c.updated_at,
			c.article_id, c.kind, c.buried_until, c.suspended
		FROM cards c JOIN decks d ON d.id = c.deck_id
		WHERE d.user_id = ? AND c.suspended = 0
		ORDER BY c.created_at
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("scan cards: %w", err)
	}
	defer rows.Close()

	var out []SplitCandidate
	for rows.Next() {
		card, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		reason := ""
		switch {
		case hasListMarkup(card.Back) && hasConjunction(card.Front):
			reason = "the answer is a list and the question asks two things"
		case hasListMarkup(card.Back):
			reason = "the answer is a list, so the card can only be failed in part"
		case hasConjunction(card.Front):
			reason = "the question joins two questions with a conjunction"
		default:
			continue
		}
		out = append(out, SplitCandidate{Card: *card, Reason: reason})
	}
	return out, rows.Err()
}

// Propose fills in what each candidate would become. Nothing is written.
func (s *SplitService) Propose(userID string, candidates []SplitCandidate, splitter CardSplitter) []SplitCandidate {
	for i := range candidates {
		if splitter == nil {
			continue
		}
		pairs, err := splitter.SplitCard(candidates[i].Card.Front, candidates[i].Card.Back, userID)
		if err != nil {
			candidates[i].Err = err.Error()
			continue
		}
		candidates[i].Proposed = pairs
	}
	return candidates
}

// Apply replaces one card, named explicitly by the operator, with its atomic
// parts. The original is suspended rather than deleted: it is the operator's own
// writing, the new cards start with no history of their own, and a suspension is
// reversible where a delete is not.
//
// It refuses to act on a card the caller has not named, which is what makes the
// confirmation per-card rather than a blanket apply.
func (s *SplitService) Apply(userID, cardID string, pairs []FlashcardPair) (int, error) {
	if len(pairs) == 0 {
		return 0, fmt.Errorf("nothing proposed for card %s", cardID)
	}

	var deckID string
	var articleID *string
	if err := s.db.QueryRow(`
		SELECT c.deck_id, c.article_id FROM cards c JOIN decks d ON d.id = c.deck_id
		WHERE c.id = ? AND d.user_id = ?`, cardID, userID).Scan(&deckID, &articleID); err != nil {
		return 0, fmt.Errorf("card not found: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	var createdIDs []string
	for _, p := range pairs {
		if p.Front == "" || p.Back == "" {
			continue
		}
		id := generateID()
		if _, err := tx.Exec(`
			INSERT INTO cards (id, deck_id, front, back, due, stability, difficulty, elapsed_days,
				scheduled_days, reps, lapses, state, last_review, created_at, updated_at, article_id, kind)
			VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, '0001-01-01T00:00:00Z', ?, ?, ?, ?)
		`, id, deckID, p.Front, p.Back, now, now, now, articleID, kindOrRecognition(p.Kind)); err != nil {
			return 0, fmt.Errorf("insert atomic card: %w", err)
		}
		createdIDs = append(createdIDs, id)
	}

	// The original leaves rotation but keeps everything it has earned.
	if _, err := tx.Exec("UPDATE cards SET suspended = 1 WHERE id = ?", cardID); err != nil {
		return 0, fmt.Errorf("suspend original: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	// The atomic cards inherit the original's tags, so a split does not drop a
	// card out of the topic it belonged to. Only the ids this call created —
	// an earlier cut selected "untagged cards in the deck" and would have
	// tagged every unrelated card that happened to have no tag yet.
	_ = s.inheritTags(userID, cardID, createdIDs)
	return len(createdIDs), nil
}

func (s *SplitService) inheritTags(userID, originalID string, newIDs []string) error {
	rows, err := s.db.Query(`
		SELECT t.key FROM tags t JOIN card_tags ct ON ct.tag_id = t.id
		WHERE ct.card_id = ? AND t.user_id = ?`, originalID, userID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return err
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil
	}

	tags := NewTagService(s.db)
	for _, id := range newIDs {
		for _, k := range keys {
			_ = tags.Attach(userID, id, k)
		}
	}
	return nil
}
