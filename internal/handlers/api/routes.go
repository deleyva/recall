package api

import "github.com/labstack/echo/v4"

// RegisterRoutes wires the whole /api/v1 surface. It is the single source of
// truth for what the API is: `recall routes` prints it, and a test diffs it
// against static/openapi.yaml so the spec cannot drift from the router.
func (h *Handler) RegisterRoutes(g *echo.Group, requireAuth echo.MiddlewareFunc) {
	// Public
	g.GET("/health", h.Health)
	g.POST("/auth/register", h.Register)
	g.POST("/auth/login", h.Login)
	g.POST("/auth/logout", h.Logout)

	a := g.Group("", requireAuth)

	// Account
	a.GET("/me", h.GetMe)
	a.PUT("/me/settings", h.UpdateMySettings)
	a.GET("/me/tokens", h.ListTokens)
	a.POST("/me/tokens", h.CreateToken)
	a.DELETE("/me/tokens/:id", h.DeleteToken)

	// Search
	a.GET("/search", h.Search)
	a.POST("/search/reindex", h.Reindex)

	// Decks
	a.GET("/decks", h.ListDecks)
	a.POST("/decks", h.CreateDeck)
	a.GET("/decks/:id", h.GetDeck)
	a.PUT("/decks/:id", h.UpdateDeck)
	a.DELETE("/decks/:id", h.DeleteDeck)
	a.GET("/decks/:id/cards", h.ListCards)
	a.POST("/decks/:id/cards", h.CreateCard)
	a.POST("/decks/:id/import", h.ImportCards)
	a.GET("/decks/:id/study", h.GetStudyCard)
	a.POST("/decks/:id/study", h.SubmitStudyReview)

	// Cards
	a.GET("/cards", h.ListAllCards)
	a.GET("/cards/:id", h.GetCard)
	a.PUT("/cards/:id", h.UpdateCard)
	a.DELETE("/cards/:id", h.DeleteCard)

	// Articles — including the stored text the UI never used to show
	a.GET("/articles", h.ListArticles)
	a.POST("/articles", h.CreateArticle)
	a.GET("/articles/:id", h.GetArticle)
	a.PUT("/articles/:id", h.UpdateArticle)
	a.DELETE("/articles/:id", h.DeleteArticle)
	a.GET("/articles/:id/content", h.GetArticleContent)
	a.GET("/articles/:id/cards", h.ListArticleCards)
	a.POST("/articles/:id/generate", h.GenerateArticleCards)
	a.GET("/articles/:id/chat", h.ListChat)
	a.POST("/articles/:id/chat", h.SendChat)
	a.DELETE("/articles/:id/chat", h.ClearChat)

	// Playlists
	a.GET("/playlists", h.ListPlaylists)
	a.POST("/playlists", h.CreatePlaylist)
	a.GET("/playlists/:id", h.GetPlaylist)
	a.DELETE("/playlists/:id", h.DeletePlaylist)
	a.POST("/playlists/:id/articles", h.LinkPlaylistArticle)
	a.DELETE("/playlists/:id/articles/:articleID", h.UnlinkPlaylistArticle)
	a.POST("/playlists/:id/decks", h.LinkPlaylistDeck)
	a.DELETE("/playlists/:id/decks/:deckID", h.UnlinkPlaylistDeck)

	// Podcasts
	a.GET("/podcasts", h.ListPodcasts)
	a.POST("/podcasts", h.CreatePodcast)
	a.GET("/podcasts/pending", h.ListPendingPodcasts)
	a.GET("/podcasts/:id", h.GetPodcast)
	a.DELETE("/podcasts/:id", h.DeletePodcast)
	a.GET("/podcasts/:id/content", h.GetPodcastContent)
	a.PUT("/podcasts/:id/status", h.UpdatePodcastStatus)

	// Stats
	a.GET("/stats", h.GetStats)
	a.GET("/stats/history", h.GetStatsHistory)
}
