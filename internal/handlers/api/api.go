package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/models"
	"github.com/deleyva/recall/internal/scheduler"
	"github.com/deleyva/recall/internal/services"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	auth      *services.AuthService
	decks     *services.DeckService
	cards     *services.CardService
	reviews   *services.ReviewService
	articles  *services.ArticleService
	gemini    *services.GeminiService
	podcasts  *services.PodcastService
	playlists *services.PlaylistService
	scheduler *scheduler.Scheduler
	authMw    *middleware.AuthMiddleware
	db        *sql.DB
}

func NewHandler(auth *services.AuthService, decks *services.DeckService, cards *services.CardService, reviews *services.ReviewService, articles *services.ArticleService, gemini *services.GeminiService, podcasts *services.PodcastService, playlists *services.PlaylistService, sched *scheduler.Scheduler, authMw *middleware.AuthMiddleware, db *sql.DB) *Handler {
	return &Handler{
		auth:      auth,
		decks:     decks,
		cards:     cards,
		reviews:   reviews,
		articles:  articles,
		gemini:    gemini,
		podcasts:  podcasts,
		playlists: playlists,
		scheduler: sched,
		authMw:    authMw,
		db:        db,
	}
}

func (h *Handler) Register(c echo.Context) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if req.Email == "" || req.Password == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "email and password required"})
	}
	if len(req.Password) < 8 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
	}

	user, err := h.auth.Register(req.Email, req.Password)
	if err != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}

	// Create session
	sess, _ := h.authMw.GetStore().Get(c.Request(), middleware.SessionName)
	sess.Values[middleware.UserIDKey] = user.ID
	sess.Values[middleware.EmailKey] = user.Email
	sess.Save(c.Request(), c.Response())

	return c.JSON(http.StatusCreated, user)
}

func (h *Handler) Login(c echo.Context) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	user, err := h.auth.Login(req.Email, req.Password)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
	}

	sess, _ := h.authMw.GetStore().Get(c.Request(), middleware.SessionName)
	sess.Values[middleware.UserIDKey] = user.ID
	sess.Values[middleware.EmailKey] = user.Email
	sess.Values[middleware.IsAdminKey] = user.IsAdmin
	sess.Save(c.Request(), c.Response())

	return c.JSON(http.StatusOK, user)
}

func (h *Handler) Logout(c echo.Context) error {
	sess, _ := h.authMw.GetStore().Get(c.Request(), middleware.SessionName)
	sess.Options.MaxAge = -1
	sess.Save(c.Request(), c.Response())
	return c.JSON(http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *Handler) ListDecks(c echo.Context) error {
	userID := middleware.GetUserID(c)
	decks, err := h.decks.List(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if decks == nil {
		decks = []models.Deck{}
	}
	return c.JSON(http.StatusOK, decks)
}

func (h *Handler) CreateDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	deck, err := h.decks.Create(userID, req.Name, req.Description)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, deck)
}

func (h *Handler) GetDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deck, err := h.decks.Get(userID, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deck not found"})
	}
	return c.JSON(http.StatusOK, deck)
}

func (h *Handler) UpdateDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if err := h.decks.Update(userID, c.Param("id"), req.Name, req.Description); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deck not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "updated"})
}

func (h *Handler) DeleteDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.decks.Delete(userID, c.Param("id")); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deck not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *Handler) ListCards(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deck not found"})
	}

	page, _ := strconv.Atoi(c.QueryParam("page"))
	perPage, _ := strconv.Atoi(c.QueryParam("per_page"))

	cards, total, err := h.cards.List(deckID, page, perPage)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"cards": cards,
		"total": total,
	})
}

func (h *Handler) CreateCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deck not found"})
	}

	var req struct {
		Front string `json:"front"`
		Back  string `json:"back"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	card, err := h.cards.Create(deckID, req.Front, req.Back, nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, card)
}

func (h *Handler) GetCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	card, err := h.cards.GetForUser(c.Param("id"), userID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "card not found"})
	}
	return c.JSON(http.StatusOK, card)
}

func (h *Handler) UpdateCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		Front string `json:"front"`
		Back  string `json:"back"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if err := h.cards.UpdateForUser(c.Param("id"), userID, req.Front, req.Back); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "card not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "updated"})
}

func (h *Handler) DeleteCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.cards.DeleteForUser(c.Param("id"), userID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "card not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *Handler) GetStudyCard(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deck not found"})
	}

	card, dueCount, err := h.reviews.GetNextDue(deckID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if card == nil {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"card":      nil,
			"due_count": 0,
		})
	}

	now := time.Now().UTC()
	intervals := h.scheduler.PreviewIntervals(*card, now)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"card":      card,
		"due_count": dueCount,
		"intervals": intervals,
	})
}

func (h *Handler) SubmitStudyReview(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	// Verify deck ownership
	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
	}

	var req struct {
		CardID string `json:"card_id"`
		Rating int    `json:"rating"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if req.Rating < 1 || req.Rating > 4 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "rating must be 1-4"})
	}

	card, err := h.cards.Get(req.CardID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "card not found"})
	}

	now := time.Now().UTC()
	updatedCard, reviewLog := h.scheduler.Schedule(*card, req.Rating, now)

	h.cards.UpdateFSRS(&updatedCard)
	h.reviews.CreateLog(reviewLog.CardID, reviewLog.Rating, reviewLog.ScheduledDays, reviewLog.ElapsedDays, reviewLog.State)

	// Get next card
	nextCard, dueCount, _ := h.reviews.GetNextDue(deckID)

	result := map[string]interface{}{
		"due_count": dueCount,
	}
	if nextCard != nil {
		intervals := h.scheduler.PreviewIntervals(*nextCard, time.Now().UTC())
		result["next_card"] = nextCard
		result["intervals"] = intervals
	}

	return c.JSON(http.StatusOK, result)
}

func (h *Handler) ImportCards(c echo.Context) error {
	userID := middleware.GetUserID(c)
	deckID := c.Param("id")

	if _, err := h.decks.Get(userID, deckID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deck not found"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no file"})
	}
	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "read file"})
	}
	defer src.Close()

	count, err := h.cards.ImportCSV(deckID, src)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"imported": count,
	})
}

func (h *Handler) GetStats(c echo.Context) error {
	userID := middleware.GetUserID(c)
	stats, err := h.reviews.GetStats(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, stats)
}

func (h *Handler) GetStatsHistory(c echo.Context) error {
	userID := middleware.GetUserID(c)
	history, err := h.reviews.GetHistory(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, history)
}

// Article API endpoints

func (h *Handler) CreateArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	// If content is provided directly, create without fetching
	if req.Content != "" {
		article, err := h.articles.CreateDirect(userID, req.URL, req.Title, req.Content)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusCreated, article)
	}

	if req.URL == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "url or content required"})
	}

	article, err := h.articles.Create(userID, req.URL)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, article)
}

func (h *Handler) ListArticles(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articles, err := h.articles.List(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if articles == nil {
		articles = []models.Article{}
	}
	return c.JSON(http.StatusOK, articles)
}

func (h *Handler) DeleteArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.articles.Delete(userID, c.Param("id")); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *Handler) GenerateArticleCards(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if !h.gemini.IsConfigured() {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Gemini not configured"})
	}

	var req struct {
		Count int `json:"count"`
	}
	if err := c.Bind(&req); err != nil || req.Count < 1 {
		req.Count = 5
	}
	if req.Count > 20 {
		req.Count = 20
	}

	article, err := h.articles.Get(userID, articleID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "article not found"})
	}

	existing, _ := h.articles.GetCardsForArticle(articleID)

	var customPrompt string
	h.db.QueryRow("SELECT flashcard_prompt FROM users WHERE id = ?", userID).Scan(&customPrompt)

	pairs, err := h.gemini.GenerateFlashcards(article.Content, existing, req.Count, customPrompt)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	deckID, err := h.articles.EnsureReadingDeck(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	count, err := h.cards.CreateBatch(deckID, &articleID, pairs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"generated": count,
	})
}

// Podcast API endpoints

func (h *Handler) ListPendingPodcasts(c echo.Context) error {
	podcasts, err := h.podcasts.ListPending()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if podcasts == nil {
		podcasts = []models.Podcast{}
	}
	return c.JSON(http.StatusOK, podcasts)
}

// Playlist API endpoints

func (h *Handler) ListPlaylists(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlists, err := h.playlists.List(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if playlists == nil {
		playlists = []models.Playlist{}
	}
	return c.JSON(http.StatusOK, playlists)
}

func (h *Handler) CreatePlaylist(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		URL         string `json:"url"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if req.URL == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "url required"})
	}

	playlist, err := h.playlists.Create(userID, req.URL, req.Title, req.Description)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, playlist)
}

func (h *Handler) GetPlaylist(c echo.Context) error {
	userID := middleware.GetUserID(c)
	playlist, err := h.playlists.Get(userID, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "playlist not found"})
	}
	return c.JSON(http.StatusOK, playlist)
}

func (h *Handler) DeletePlaylist(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.playlists.Delete(userID, c.Param("id")); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "playlist not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *Handler) LinkPlaylistArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		ArticleID string `json:"article_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if err := h.playlists.LinkArticle(userID, c.Param("id"), req.ArticleID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "linked"})
}

func (h *Handler) UnlinkPlaylistArticle(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.playlists.UnlinkArticle(userID, c.Param("id"), c.Param("articleID")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "unlinked"})
}

func (h *Handler) LinkPlaylistDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		DeckID string `json:"deck_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if err := h.playlists.LinkDeck(userID, c.Param("id"), req.DeckID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "linked"})
}

func (h *Handler) UnlinkPlaylistDeck(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.playlists.UnlinkDeck(userID, c.Param("id"), c.Param("deckID")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "unlinked"})
}

func (h *Handler) UpdatePodcastStatus(c echo.Context) error {
	podcastID := c.Param("id")
	var req struct {
		Status     string `json:"status"`
		AudioURL   string `json:"audio_url"`
		NotebookID string `json:"notebook_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	validStatuses := map[string]bool{
		models.PodcastStatusProcessing: true,
		models.PodcastStatusCompleted:  true,
		models.PodcastStatusFailed:     true,
	}
	if !validStatuses[req.Status] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid status"})
	}

	if err := h.podcasts.UpdateStatus(podcastID, req.Status, req.AudioURL, req.NotebookID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "updated"})
}
