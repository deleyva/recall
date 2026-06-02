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

type Card struct {
	ID            string    `json:"id"`
	DeckID        string    `json:"deck_id"`
	ArticleID     *string   `json:"article_id,omitempty"`
	Front         string    `json:"front"`
	Back          string    `json:"back"`
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

type LearningGoal struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Title       string    `json:"title"`
	SeedURL     string    `json:"seed_url"`
	SeedTitle   string    `json:"seed_title"`
	SeedLang    string    `json:"seed_lang"`
	TimeHorizon int       `json:"time_horizon"`
	DailyPace   int       `json:"daily_pace"`
	Status      string    `json:"status"`
	ThumbURL    string    `json:"thumb_url"`
	CreatedAt   time.Time `json:"created_at"`
	// Computed
	TotalNodes  int `json:"total_nodes,omitempty"`
	QueuedNodes int `json:"queued_nodes,omitempty"`
	AddedNodes  int `json:"added_nodes,omitempty"`
	DayNumber   int `json:"day_number,omitempty"`
}

type GraphNode struct {
	ID             string    `json:"id"`
	GoalID         string    `json:"goal_id"`
	WikiTitle      string    `json:"wiki_title"`
	WikiURL        string    `json:"wiki_url"`
	Summary        string    `json:"summary"`
	ThumbURL       string    `json:"thumb_url"`
	Depth          int       `json:"depth"`
	Status         string    `json:"status"`
	ArticleID      *string   `json:"article_id,omitempty"`
	ScheduledDay   *int      `json:"scheduled_day,omitempty"`
	SortOrder      int       `json:"sort_order"`
	RelevanceScore float64   `json:"relevance_score"`
	CreatedAt      time.Time `json:"created_at"`
}

type GraphEdge struct {
	ID           string `json:"id"`
	GoalID       string `json:"goal_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
}
