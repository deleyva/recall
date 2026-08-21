package models

import "time"

type User struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	PasswordHash     string    `json:"-"`
	DailyCardLimit   int       `json:"daily_card_limit"`
	ReadeckURL       string    `json:"-"`
	ReadeckAPIToken  string    `json:"-"`
	IsAdmin          bool      `json:"is_admin"`
	CreatedAt        time.Time `json:"created_at"`
}

type Deck struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	DueCount    int       `json:"due_count,omitempty"` // computed field
}

type Article struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	URL            string    `json:"url"`
	Title          string    `json:"title"`
	Domain         string    `json:"domain"`
	Content        string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	FlashcardCount int       `json:"flashcard_count,omitempty"`
}

// ArticleDetail is an Article with its stored text. Kept separate from Article
// so list endpoints stay small — only single-article reads carry the body.
type ArticleDetail struct {
	Article
	Content string `json:"content"`
}

const (
	SearchKindArticle   = "article"
	SearchKindFlashcard = "flashcard"
	SearchKindChat      = "chat"
)

// SearchResult is one hit from the full-text index. Snippet is pre-escaped HTML
// with the matched terms wrapped in <mark>.
type SearchResult struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet"`
	URL       string  `json:"url,omitempty"`
	ArticleID string  `json:"article_id,omitempty"`
	DeckID    string  `json:"deck_id,omitempty"`
	Score     float64 `json:"score"`
}

type SearchResponse struct {
	Query   string         `json:"query"`
	Total   int            `json:"total"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
	Results []SearchResult `json:"results"`
}

type Card struct {
	ID            string    `json:"id"`
	DeckID        string    `json:"deck_id"`
	ArticleID     *string   `json:"article_id,omitempty"`
	Front         string    `json:"front"`
	Back          string    `json:"back"`
	Kind          string    `json:"kind"`
	Due           time.Time `json:"due"`
	Stability     float64   `json:"stability"`
	Difficulty    float64   `json:"difficulty"`
	ElapsedDays   int       `json:"elapsed_days"`
	ScheduledDays int       `json:"scheduled_days"`
	Reps          int       `json:"reps"`
	Lapses        int       `json:"lapses"`
	State         int       `json:"state"`
	LastReview    time.Time `json:"last_review"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ReviewLog struct {
	ID            string    `json:"id"`
	CardID        string    `json:"card_id"`
	Rating        int       `json:"rating"`
	ScheduledDays int       `json:"scheduled_days"`
	ElapsedDays   int       `json:"elapsed_days"`
	ReviewTime    time.Time `json:"review_time"`
	State         int       `json:"state"`
}

type Stats struct {
	TotalCards int `json:"total_cards"`
	DueToday   int `json:"due_today"`
	Streak     int `json:"streak"`
}

type DailyReviewCount struct {
	Date    string `json:"date"`
	Reviews int    `json:"reviews"`
}

type APIToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type WikiImage struct {
	URL         string `json:"url"`
	ThumbURL    string `json:"thumb_url"`
	Description string `json:"description"`
	Title       string `json:"title"`
}

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type ChatMessage struct {
	ID        string    `json:"id"`
	ArticleID string    `json:"article_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	PodcastStatusPending    = "pending"
	PodcastStatusProcessing = "processing"
	PodcastStatusCompleted  = "completed"
	PodcastStatusFailed     = "failed"
)

type Podcast struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	AudioURL     string    `json:"audio_url,omitempty"`
	NotebookID   string    `json:"notebook_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	ArticleCount int       `json:"article_count,omitempty"`
	Articles     []Article `json:"articles,omitempty"`
}

type PodcastArticle struct {
	PodcastID string `json:"podcast_id"`
	ArticleID string `json:"article_id"`
}

const (
	PlaylistTypeSpotify  = "spotify"
	PlaylistTypeYouTube  = "youtube"
)

type Playlist struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	Type        string    `json:"type"`
	ExternalID  string    `json:"external_id"`
	CreatedAt   time.Time `json:"created_at"`
	Articles    []Article `json:"articles,omitempty"`
	Decks       []Deck    `json:"decks,omitempty"`
}

func (p Playlist) EmbedURL() string {
	switch p.Type {
	case PlaylistTypeSpotify:
		return "https://open.spotify.com/embed/playlist/" + p.ExternalID
	case PlaylistTypeYouTube:
		return "https://www.youtube.com/embed/videoseries?list=" + p.ExternalID
	}
	return p.URL
}

type PlaylistArticle struct {
	PlaylistID string `json:"playlist_id"`
	ArticleID  string `json:"article_id"`
}

type PlaylistDeck struct {
	PlaylistID string `json:"playlist_id"`
	DeckID     string `json:"deck_id"`
}



