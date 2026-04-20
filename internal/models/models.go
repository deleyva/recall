package models

import "time"

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
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
