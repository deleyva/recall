package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/deleyva/recall/internal/config"
	"github.com/deleyva/recall/internal/database"
	"github.com/deleyva/recall/internal/handlers/api"
	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/handlers/web"
	"github.com/deleyva/recall/internal/scheduler"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	"github.com/pressly/goose/v3"
	"github.com/robfig/cron/v3"
)

func main() {
	cfg := config.Load()

	// Open database
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := goose.SetDialect("sqlite3"); err != nil {
		log.Fatalf("Failed to set goose dialect: %v", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Handle CLI subcommands
	if len(os.Args) > 1 {
		authSvc := services.NewAuthService(db)
		switch os.Args[1] {
		case "reset-password":
			if len(os.Args) < 4 {
				fmt.Println("Usage: recall reset-password <email> <new-password>")
				os.Exit(1)
			}
			if err := authSvc.ResetPassword(os.Args[2], os.Args[3]); err != nil {
				log.Fatalf("Failed: %v", err)
			}
			fmt.Printf("Password reset for %s\n", os.Args[2])
			return
		case "list-users":
			emails, err := authSvc.ListUsers()
			if err != nil {
				log.Fatalf("Failed: %v", err)
			}
			for _, e := range emails {
				fmt.Println(e)
			}
			return
		case "create-token":
			if len(os.Args) < 4 {
				fmt.Println("Usage: recall create-token <email> <token-name>")
				os.Exit(1)
			}
			tokenSvc := services.NewTokenService(db)
			var userID string
			err := db.QueryRow("SELECT id FROM users WHERE email = ?", os.Args[2]).Scan(&userID)
			if err != nil {
				log.Fatalf("User not found: %s", os.Args[2])
			}
			rawToken, _, err := tokenSvc.Create(userID, os.Args[3])
			if err != nil {
				log.Fatalf("Failed: %v", err)
			}
			fmt.Println(rawToken)
			return
		case "reindex":
			count, err := services.NewSearchService(db).Reindex()
			if err != nil {
				log.Fatalf("Failed: %v", err)
			}
			fmt.Printf("Search index rebuilt: %d entries\n", count)
			return
		case "routes":
			e := echo.New()
			(&api.Handler{}).RegisterRoutes(e.Group("/api/v1"), func(next echo.HandlerFunc) echo.HandlerFunc { return next })
			for _, r := range e.Routes() {
				fmt.Printf("%-7s %s\n", r.Method, r.Path)
			}
			return
		case "set-admin":
			if len(os.Args) < 3 {
				fmt.Println("Usage: recall set-admin <email>")
				os.Exit(1)
			}
			result, err := db.Exec("UPDATE users SET is_admin = 1 WHERE email = ?", os.Args[2])
			if err != nil {
				log.Fatalf("Failed: %v", err)
			}
			rows, _ := result.RowsAffected()
			if rows == 0 {
				log.Fatalf("User not found: %s", os.Args[2])
			}
			fmt.Printf("User %s is now admin\n", os.Args[2])
			return
		case "metrics":
			if len(os.Args) < 3 {
				fmt.Println("Usage: recall metrics <email> [--json] [--tz <Zone>]")
				os.Exit(1)
			}
			email := os.Args[2]
			asJSON := false
			loc := time.Local
			for i := 3; i < len(os.Args); i++ {
				switch os.Args[i] {
				case "--json":
					asJSON = true
				case "--tz":
					if i+1 >= len(os.Args) {
						log.Fatal("--tz needs a zone name, e.g. --tz Europe/Madrid")
					}
					i++
					l, err := time.LoadLocation(os.Args[i])
					if err != nil {
						log.Fatalf("Unknown timezone %q: %v", os.Args[i], err)
					}
					loc = l
				default:
					log.Fatalf("Unknown argument: %s", os.Args[i])
				}
			}
			m, err := services.NewMetricsService(db).Compute(email, loc)
			if err != nil {
				log.Fatalf("Failed: %v", err)
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(m); err != nil {
					log.Fatalf("Failed: %v", err)
				}
				return
			}
			if err := services.RenderMetrics(os.Stdout, m); err != nil {
				log.Fatalf("Failed: %v", err)
			}
			return
		}
	}

	// Initialize services
	authService := services.NewAuthService(db)
	deckService := services.NewDeckService(db)
	cardService := services.NewCardService(db)
	reviewService := services.NewReviewService(db)
	articleService := services.NewArticleService(db)
	llmService := services.NewLLMService(cfg.LLMAPIKey, cfg.LLMAPIURL, cfg.LLMModel, db)
	tokenService := services.NewTokenService(db)
	wikipediaService := services.NewWikipediaService()
	chatService := services.NewChatService(db)
	searchService := services.NewSearchService(db)
	podcastService := services.NewPodcastService(db)
	playlistService := services.NewPlaylistService(db)
	cronService := services.NewCronService(db, articleService, cardService, llmService, podcastService)
	readeckService := services.NewReadeckService(db, articleService)
	sched := scheduler.New()

	// Session store
	store := sessions.NewCookieStore([]byte(cfg.SessionKey))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}

	authMw := middleware.NewAuthMiddleware(store)
	authMw.SetTokenService(tokenService)

	// Parse templates (each page gets its own template set to avoid content block conflicts)
	tmpl := templates.NewRegistry()
	if err := tmpl.Load("templates"); err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}

	// Web handlers
	authHandler := web.NewAuthHandler(authService, store, tmpl)
	deckHandler := web.NewDeckHandler(deckService, reviewService, tmpl)
	cardHandler := web.NewCardHandler(cardService, deckService, tmpl)
	reviewHandler := web.NewReviewHandler(reviewService, cardService, deckService, sched, tmpl)
	articleHandler := web.NewArticleHandler(articleService, cardService, deckService, llmService, wikipediaService, tmpl, db)
	chatHandler := web.NewChatHandler(articleService, chatService, llmService, tmpl)
	podcastHandler := web.NewPodcastHandler(podcastService, articleService, tmpl)
	playlistHandler := web.NewPlaylistHandler(playlistService, articleService, deckService, tmpl)
	profileHandler := web.NewProfileHandler(tokenService, llmService, tmpl, db)
	searchHandler := web.NewSearchHandler(searchService, tmpl)

	// API handler
	apiHandler := api.NewHandler(api.Deps{
		Auth:      authService,
		Decks:     deckService,
		Cards:     cardService,
		Reviews:   reviewService,
		Articles:  articleService,
		LLM:       llmService,
		Podcasts:  podcastService,
		Playlists: playlistService,
		Chat:      chatService,
		Search:    searchService,
		Tokens:    tokenService,
		Scheduler: sched,
		AuthMw:    authMw,
		DB:        db,
	})

	// Start cron jobs (disabled in dev mode)
	c := cron.New()
	if !cfg.DevMode {
		c.AddFunc("0 1 * * *", cronService.GenerateDailyCards)
		c.AddFunc("0 2 * * *", cronService.GenerateDailyPodcasts)
		c.AddFunc("*/15 * * * *", readeckService.SyncAllUsers)
		c.Start()
		defer c.Stop()
		log.Println("Cron scheduler started (cards at 1:00 AM, podcasts at 2:00 AM, Readeck sync every 15 min)")
	} else {
		log.Println("Dev mode: cron jobs disabled")
	}

	// Echo server
	e := echo.New()
	e.HideBanner = true
	e.Use(echomw.Logger())
	e.Use(echomw.Recover())

	// Static files
	e.Static("/static", "static")
	e.File("/apple-touch-icon.png", "static/icons/apple-touch-icon.png")
	e.File("/apple-touch-icon-precomposed.png", "static/icons/apple-touch-icon.png")

	// Public routes
	e.GET("/login", authHandler.LoginPage)
	e.POST("/login", authHandler.Login)
	e.GET("/register", authHandler.RegisterPage)
	e.POST("/register", authHandler.Register)
	e.POST("/logout", authHandler.Logout)

	// Docs (public)
	e.GET("/docs", func(c echo.Context) error {
		return tmpl.ExecuteTemplate(c.Response(), "docs.html", nil)
	})

	// Protected web routes
	auth := e.Group("", authMw.RequireAuth)
	auth.GET("/", deckHandler.Dashboard)
	auth.GET("/decks/new", deckHandler.NewDeckPage)
	auth.POST("/decks", deckHandler.CreateDeck)
	auth.GET("/decks/:id", deckHandler.ViewDeck)
	auth.GET("/decks/:id/edit", deckHandler.EditDeckPage)
	auth.POST("/decks/:id", deckHandler.UpdateDeck)
	auth.POST("/decks/:id/delete", deckHandler.DeleteDeck)
	auth.GET("/decks/:id/cards", cardHandler.ListCards)
	auth.GET("/decks/:id/cards/new", cardHandler.NewCardPage)
	auth.POST("/decks/:id/cards", cardHandler.CreateCard)
	auth.GET("/decks/:id/cards/:cardID/edit", cardHandler.EditCardPage)
	auth.POST("/decks/:id/cards/:cardID", cardHandler.UpdateCard)
	auth.POST("/decks/:id/cards/:cardID/delete", cardHandler.DeleteCard)
	auth.POST("/decks/:id/import", cardHandler.ImportCSV)
	auth.GET("/decks/:id/study", reviewHandler.StudyPage)
	auth.GET("/decks/:id/study/:cardID/answer", reviewHandler.ShowAnswer)
	auth.GET("/decks/:id/study/:cardID/edit", reviewHandler.StudyEditCard)
	auth.PUT("/decks/:id/study/:cardID", reviewHandler.StudyUpdateCard)
	auth.DELETE("/decks/:id/study/:cardID", reviewHandler.StudyDeleteCard)
	auth.POST("/decks/:id/study", reviewHandler.SubmitReview)
	auth.GET("/search", searchHandler.SearchPage)
	auth.GET("/search/results", searchHandler.SearchResults)
	auth.GET("/to-read", articleHandler.ListPage)
	auth.POST("/to-read", articleHandler.AddArticle)
	auth.POST("/to-read/:id/delete", articleHandler.DeleteArticle)
	auth.POST("/to-read/:id/generate", articleHandler.GenerateFlashcards)
	auth.GET("/to-read/:id/read", articleHandler.ReadArticle)
	auth.GET("/to-read/:id/images", articleHandler.ArticleImages)
	auth.GET("/to-read/:id/viewer", articleHandler.ImageViewer)
	auth.GET("/to-read/:id/chat", chatHandler.ChatPage)
	auth.POST("/to-read/:id/chat", chatHandler.SendMessage)
	auth.POST("/to-read/:id/chat/clear", chatHandler.ClearChat)
	auth.GET("/playlists", playlistHandler.ListPage)
	auth.GET("/playlists/:id", playlistHandler.DetailPage)
	auth.POST("/playlists", playlistHandler.Create)
	auth.POST("/playlists/:id/delete", playlistHandler.Delete)
	auth.POST("/playlists/:id/link-article", playlistHandler.LinkArticle)
	auth.POST("/playlists/:id/unlink-article/:articleID", playlistHandler.UnlinkArticle)
	auth.POST("/playlists/:id/link-deck", playlistHandler.LinkDeck)
	auth.POST("/playlists/:id/unlink-deck/:deckID", playlistHandler.UnlinkDeck)
	auth.GET("/podcasts", podcastHandler.ListPage)
	auth.POST("/podcasts", podcastHandler.CreatePodcast)
	auth.POST("/podcasts/:id/delete", podcastHandler.DeletePodcast)
	auth.GET("/profile", profileHandler.ProfilePage)
	auth.POST("/profile/settings", profileHandler.UpdateSettings)
	auth.POST("/profile/tokens", profileHandler.CreateToken)
	auth.POST("/profile/tokens/:id/delete", profileHandler.DeleteToken)
	auth.GET("/stats", reviewHandler.StatsPage)

	// API routes — the full surface lives in api.RegisterRoutes
	apiHandler.RegisterRoutes(e.Group("/api/v1"), authMw.RequireAuth)

	log.Printf("Recall starting on :%s", cfg.Port)
	e.Logger.Fatal(e.Start(fmt.Sprintf(":%s", cfg.Port)))
}
