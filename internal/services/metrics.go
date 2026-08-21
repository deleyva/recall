package services

import (
	"database/sql"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

// LeechThreshold is the number of lapses at which Anki calls a card a leech and
// suspends it. Recall does not act on it yet; the metric exists so the decision
// to add that behaviour can be made against a number rather than a hunch.
const LeechThreshold = 8

// conjunctions marks a front that asks two things at once. Only coordinating
// conjunctions are listed: a disjunction ("X o Y") is usually one question with
// alternatives, while a conjunction is two questions wearing one card.
var conjunctions = []string{"y", "e", "and"}

// punctuation is flattened to spaces before conjunction matching so that
// "X, y Z" is detected the same as "X y Z".
var punctuationReplacer = strings.NewReplacer(
	",", " ", ";", " ", ":", " ", ".", " ",
	"¿", " ", "?", " ", "¡", " ", "!", " ",
	"(", " ", ")", " ", "—", " ", "–", " ", "-", " ",
	"\"", " ", "'", " ", "«", " ", "»", " ", "\n", " ", "\t", " ",
)

type Corpus struct {
	Decks       int    `json:"decks"`
	Cards       int    `json:"cards"`
	Articles    int    `json:"articles"`
	Reviews     int    `json:"reviews"`
	ActiveDays  int    `json:"active_days"`
	FirstReview string `json:"first_review"`
	LastReview  string `json:"last_review"`
}

// Retention is measured over spaced reviews only. A card's first exposure has
// nothing to say about whether it was remembered.
type Retention struct {
	SpacedReviews int     `json:"spaced_reviews"`
	Again         int     `json:"again"`
	RetentionPct  float64 `json:"retention_pct"`
}

type RatingCount struct {
	Rating int     `json:"rating"`
	Label  string  `json:"label"`
	Count  int     `json:"count"`
	Pct    float64 `json:"pct"`
}

// RatingFollowup answers whether a rating carries information: of the reviews
// that pressed this button and were later reviewed again, how many of those
// next reviews were failures. A middle button that predicts failure no better
// than "Good" is noise the scheduler cannot use.
type RatingFollowup struct {
	Rating       int     `json:"rating"`
	Label        string  `json:"label"`
	Followups    int     `json:"followups"`
	NextAgain    int     `json:"next_again"`
	NextAgainPct float64 `json:"next_again_pct"`
}

type LeechStats struct {
	Threshold int `json:"threshold"`
	Count     int `json:"count"`
	MaxLapses int `json:"max_lapses"`
}

// SiblingStats measures how often cards born from the same article are reviewed
// on the same day, which is the condition under which the first card primes the
// rest and the session reports a recall it did not earn.
type SiblingStats struct {
	ArticleReviews int     `json:"article_reviews"`
	WithSibling    int     `json:"with_sibling"`
	Pct            float64 `json:"pct"`
	MaxPerDay      int     `json:"max_per_day"`
}

type Share struct {
	Count int     `json:"count"`
	Total int     `json:"total"`
	Pct   float64 `json:"pct"`
}

type DeckCards struct {
	Name  string `json:"name"`
	Cards int    `json:"cards"`
}

type HourReviews struct {
	Hour    int `json:"hour"`
	Reviews int `json:"reviews"`
}

type Metrics struct {
	User           string           `json:"user"`
	Timezone       string           `json:"timezone"`
	Corpus         Corpus           `json:"corpus"`
	TrueRetention  Retention        `json:"true_retention"`
	Ratings        []RatingCount    `json:"ratings"`
	RatingFollowup []RatingFollowup `json:"rating_followup"`
	Leeches        LeechStats       `json:"leeches"`
	SiblingSameDay SiblingStats     `json:"sibling_same_day"`
	ListBacks      Share            `json:"list_backs"`
	CompoundFronts Share            `json:"compound_fronts"`
	Decks          []DeckCards      `json:"decks"`
	Hours          []HourReviews    `json:"hours"`
}

type MetricsService struct {
	db *sql.DB
}

func NewMetricsService(db *sql.DB) *MetricsService {
	return &MetricsService{db: db}
}

var ratingLabels = map[int]string{1: "again", 2: "hard", 3: "good", 4: "easy"}

// Compute reads the whole of one user's card and review history and derives the
// measures the memory-fidelity work is judged by. It is read-only, and every
// ordering is explicit so two runs against an unchanged database produce
// identical output.
func (s *MetricsService) Compute(email string, loc *time.Location) (*Metrics, error) {
	if loc == nil {
		loc = time.UTC
	}

	var userID string
	if err := s.db.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no such user: %s", email)
		}
		return nil, fmt.Errorf("look up user: %w", err)
	}

	m := &Metrics{User: email, Timezone: loc.String()}

	if err := s.readCards(userID, m); err != nil {
		return nil, err
	}
	if err := s.readReviews(userID, loc, m); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM articles WHERE user_id = ?", userID).Scan(&m.Corpus.Articles); err != nil {
		return nil, fmt.Errorf("count articles: %w", err)
	}
	return m, nil
}

// readCards fills every card-derived measure in one pass.
func (s *MetricsService) readCards(userID string, m *Metrics) error {
	rows, err := s.db.Query(`
		SELECT d.name, c.front, c.back, c.lapses
		FROM cards c
		JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ?
		ORDER BY c.id`, userID)
	if err != nil {
		return fmt.Errorf("read cards: %w", err)
	}
	defer rows.Close()

	perDeck := map[string]int{}
	for rows.Next() {
		var deck, front, back string
		var lapses int
		if err := rows.Scan(&deck, &front, &back, &lapses); err != nil {
			return fmt.Errorf("scan card: %w", err)
		}
		m.Corpus.Cards++
		perDeck[deck]++

		if hasListMarkup(back) {
			m.ListBacks.Count++
		}
		if hasConjunction(front) {
			m.CompoundFronts.Count++
		}
		if lapses >= LeechThreshold {
			m.Leeches.Count++
		}
		if lapses > m.Leeches.MaxLapses {
			m.Leeches.MaxLapses = lapses
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate cards: %w", err)
	}

	// Empty decks still count as decks, so read them separately rather than
	// inferring the deck list from the cards.
	if err := s.db.QueryRow("SELECT COUNT(*) FROM decks WHERE user_id = ?", userID).Scan(&m.Corpus.Decks); err != nil {
		return fmt.Errorf("count decks: %w", err)
	}

	m.Leeches.Threshold = LeechThreshold
	m.ListBacks.Total = m.Corpus.Cards
	m.ListBacks.Pct = pct(m.ListBacks.Count, m.ListBacks.Total)
	m.CompoundFronts.Total = m.Corpus.Cards
	m.CompoundFronts.Pct = pct(m.CompoundFronts.Count, m.CompoundFronts.Total)

	m.Decks = make([]DeckCards, 0, len(perDeck))
	for name, n := range perDeck {
		m.Decks = append(m.Decks, DeckCards{Name: name, Cards: n})
	}
	sort.Slice(m.Decks, func(i, j int) bool {
		if m.Decks[i].Cards != m.Decks[j].Cards {
			return m.Decks[i].Cards > m.Decks[j].Cards
		}
		return m.Decks[i].Name < m.Decks[j].Name
	})
	return nil
}

// readReviews fills every review-derived measure in one ordered pass. Day
// boundaries and hours use the requested zone, not UTC — a session that ends at
// half past midnight local time belongs to the day the learner thinks it does.
func (s *MetricsService) readReviews(userID string, loc *time.Location, m *Metrics) error {
	rows, err := s.db.Query(`
		SELECT r.card_id, COALESCE(c.article_id, ''), r.rating, r.elapsed_days, r.review_time
		FROM review_logs r
		JOIN cards c ON r.card_id = c.id
		JOIN decks d ON c.deck_id = d.id
		WHERE d.user_id = ?
		ORDER BY r.review_time ASC, r.id ASC`, userID)
	if err != nil {
		return fmt.Errorf("read reviews: %w", err)
	}
	defer rows.Close()

	ratingCounts := map[int]int{}
	followups := map[int]int{}
	nextAgain := map[int]int{}
	prevRating := map[string]int{} // card id -> rating of its previous review
	perDayArticle := map[string]int{}
	days := map[string]bool{}
	hours := make([]int, 24)

	var first, last string
	for rows.Next() {
		var cardID, articleID, reviewTime string
		var rating, elapsed int
		if err := rows.Scan(&cardID, &articleID, &rating, &elapsed, &reviewTime); err != nil {
			return fmt.Errorf("scan review: %w", err)
		}

		at, err := time.Parse(time.RFC3339, reviewTime)
		if err != nil {
			return fmt.Errorf("parse review_time %q: %w", reviewTime, err)
		}
		local := at.In(loc)
		day := local.Format("2006-01-02")

		m.Corpus.Reviews++
		ratingCounts[rating]++
		days[day] = true
		hours[local.Hour()]++
		if first == "" {
			first = day
		}
		last = day

		if elapsed > 0 {
			m.TrueRetention.SpacedReviews++
			if rating == 1 {
				m.TrueRetention.Again++
			}
		}

		// The previous review of this card now has a known outcome.
		if prev, ok := prevRating[cardID]; ok {
			followups[prev]++
			if rating == 1 {
				nextAgain[prev]++
			}
		}
		prevRating[cardID] = rating

		if articleID != "" {
			m.SiblingSameDay.ArticleReviews++
			perDayArticle[day+"\x00"+articleID]++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate reviews: %w", err)
	}

	m.Corpus.ActiveDays = len(days)
	m.Corpus.FirstReview = first
	m.Corpus.LastReview = last
	m.TrueRetention.RetentionPct = pct(m.TrueRetention.SpacedReviews-m.TrueRetention.Again, m.TrueRetention.SpacedReviews)

	m.Ratings = make([]RatingCount, 0, 4)
	m.RatingFollowup = make([]RatingFollowup, 0, 4)
	for rating := 1; rating <= 4; rating++ {
		m.Ratings = append(m.Ratings, RatingCount{
			Rating: rating,
			Label:  ratingLabels[rating],
			Count:  ratingCounts[rating],
			Pct:    pct(ratingCounts[rating], m.Corpus.Reviews),
		})
		m.RatingFollowup = append(m.RatingFollowup, RatingFollowup{
			Rating:       rating,
			Label:        ratingLabels[rating],
			Followups:    followups[rating],
			NextAgain:    nextAgain[rating],
			NextAgainPct: pct(nextAgain[rating], followups[rating]),
		})
	}

	for _, n := range perDayArticle {
		if n >= 2 {
			m.SiblingSameDay.WithSibling += n
		}
		if n > m.SiblingSameDay.MaxPerDay {
			m.SiblingSameDay.MaxPerDay = n
		}
	}
	m.SiblingSameDay.Pct = pct(m.SiblingSameDay.WithSibling, m.SiblingSameDay.ArticleReviews)

	m.Hours = make([]HourReviews, 24)
	for h := 0; h < 24; h++ {
		m.Hours[h] = HourReviews{Hour: h, Reviews: hours[h]}
	}
	return nil
}

func hasListMarkup(back string) bool {
	lower := strings.ToLower(back)
	return strings.Contains(lower, "<li")
}

func hasConjunction(front string) bool {
	flat := " " + strings.ToLower(punctuationReplacer.Replace(front)) + " "
	for _, c := range conjunctions {
		if strings.Contains(flat, " "+c+" ") {
			return true
		}
	}
	return false
}

// pct returns a percentage rounded to one decimal, and zero rather than NaN when
// there is nothing to divide by.
func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(n)*1000/float64(total)) / 10
}

// RenderMetrics writes the human-readable report. It contains no timestamp and
// iterates no map, so the same database yields the same bytes every time.
func RenderMetrics(w io.Writer, m *Metrics) error {
	b := &strings.Builder{}

	fmt.Fprintf(b, "Recall metrics — %s\n", m.User)
	fmt.Fprintf(b, "timezone: %s\n\n", m.Timezone)

	fmt.Fprintf(b, "CORPUS\n")
	fmt.Fprintf(b, "  decks                    %d\n", m.Corpus.Decks)
	fmt.Fprintf(b, "  cards                    %d\n", m.Corpus.Cards)
	fmt.Fprintf(b, "  articles                 %d\n", m.Corpus.Articles)
	fmt.Fprintf(b, "  reviews                  %d\n", m.Corpus.Reviews)
	fmt.Fprintf(b, "  active days              %d\n", m.Corpus.ActiveDays)
	fmt.Fprintf(b, "  range                    %s .. %s\n\n", orDash(m.Corpus.FirstReview), orDash(m.Corpus.LastReview))

	fmt.Fprintf(b, "TRUE RETENTION  (spaced reviews only, elapsed_days > 0)\n")
	fmt.Fprintf(b, "  spaced reviews           %d\n", m.TrueRetention.SpacedReviews)
	fmt.Fprintf(b, "  again                    %d\n", m.TrueRetention.Again)
	fmt.Fprintf(b, "  retention                %.1f%%\n\n", m.TrueRetention.RetentionPct)

	fmt.Fprintf(b, "RATINGS                  count      share    next review is Again\n")
	for i, r := range m.Ratings {
		f := m.RatingFollowup[i]
		fmt.Fprintf(b, "  %-22s %5d %8.1f%%   %6.1f%% of %d\n",
			r.Label, r.Count, r.Pct, f.NextAgainPct, f.Followups)
	}
	b.WriteString("\n")

	fmt.Fprintf(b, "LEECHES  (>= %d lapses)\n", m.Leeches.Threshold)
	fmt.Fprintf(b, "  cards at threshold       %d\n", m.Leeches.Count)
	fmt.Fprintf(b, "  highest lapse count      %d\n\n", m.Leeches.MaxLapses)

	fmt.Fprintf(b, "SIBLING INTERFERENCE  (cards from the same article reviewed the same day)\n")
	fmt.Fprintf(b, "  article-linked reviews   %d\n", m.SiblingSameDay.ArticleReviews)
	fmt.Fprintf(b, "  sharing a day            %d  (%.1f%%)\n", m.SiblingSameDay.WithSibling, m.SiblingSameDay.Pct)
	fmt.Fprintf(b, "  most in one day          %d\n\n", m.SiblingSameDay.MaxPerDay)

	fmt.Fprintf(b, "FORMULATION\n")
	fmt.Fprintf(b, "  %-25s %d / %d  (%.1f%%)\n", "backs with a list", m.ListBacks.Count, m.ListBacks.Total, m.ListBacks.Pct)
	fmt.Fprintf(b, "  %-25s %d / %d  (%.1f%%)\n\n", "fronts with a conjunction", m.CompoundFronts.Count, m.CompoundFronts.Total, m.CompoundFronts.Pct)

	fmt.Fprintf(b, "DECKS\n")
	if len(m.Decks) == 0 {
		fmt.Fprintf(b, "  (none)\n")
	}
	for _, d := range m.Decks {
		fmt.Fprintf(b, "  %-24s %5d\n", truncate(d.Name, 24), d.Cards)
	}
	b.WriteString("\n")

	fmt.Fprintf(b, "REVIEWS BY HOUR  (%s)\n", m.Timezone)
	peak := 0
	for _, h := range m.Hours {
		if h.Reviews > peak {
			peak = h.Reviews
		}
	}
	for _, h := range m.Hours {
		if h.Reviews == 0 {
			continue
		}
		fmt.Fprintf(b, "  %02d  %5d  %s\n", h.Hour, h.Reviews, bar(h.Reviews, peak))
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func bar(n, peak int) string {
	if peak <= 0 {
		return ""
	}
	width := n * 40 / peak
	if width == 0 && n > 0 {
		width = 1
	}
	return strings.Repeat("█", width)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
