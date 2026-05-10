package services

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type PlaylistService struct {
	db *sql.DB
}

func NewPlaylistService(db *sql.DB) *PlaylistService {
	return &PlaylistService{db: db}
}

func (s *PlaylistService) Create(userID, rawURL, title, description string) (*models.Playlist, error) {
	pType, externalID, err := parsePlaylistURL(rawURL)
	if err != nil {
		return nil, err
	}

	id := generateID()
	now := time.Now().UTC()

	_, err = s.db.Exec(`
		INSERT INTO playlists (id, user_id, title, description, url, type, external_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, userID, title, description, rawURL, pType, externalID, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("create playlist: %w", err)
	}

	return &models.Playlist{
		ID:         id,
		UserID:     userID,
		Title:      title,
		Description: description,
		URL:        rawURL,
		Type:       pType,
		ExternalID: externalID,
		CreatedAt:  now,
	}, nil
}

func (s *PlaylistService) List(userID string) ([]models.Playlist, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, title, description, url, type, external_id, created_at
		FROM playlists
		WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	defer rows.Close()

	var playlists []models.Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		playlists = append(playlists, *p)
	}
	return playlists, nil
}

func (s *PlaylistService) Get(userID, playlistID string) (*models.Playlist, error) {
	row := s.db.QueryRow(`
		SELECT id, user_id, title, description, url, type, external_id, created_at
		FROM playlists
		WHERE id = ? AND user_id = ?
	`, playlistID, userID)

	p, err := scanPlaylist(row)
	if err != nil {
		return nil, fmt.Errorf("get playlist: %w", err)
	}

	// Load linked articles
	artRows, err := s.db.Query(`
		SELECT a.id, a.user_id, a.url, a.title, a.domain, a.created_at
		FROM articles a
		JOIN playlist_articles pa ON pa.article_id = a.id
		WHERE pa.playlist_id = ?
	`, playlistID)
	if err == nil {
		defer artRows.Close()
		for artRows.Next() {
			var a models.Article
			var cat string
			if err := artRows.Scan(&a.ID, &a.UserID, &a.URL, &a.Title, &a.Domain, &cat); err == nil {
				a.CreatedAt, _ = time.Parse(time.RFC3339, cat)
				p.Articles = append(p.Articles, a)
			}
		}
	}

	// Load linked decks
	deckRows, err := s.db.Query(`
		SELECT d.id, d.user_id, d.name, d.description, d.created_at
		FROM decks d
		JOIN playlist_decks pd ON pd.deck_id = d.id
		WHERE pd.playlist_id = ?
	`, playlistID)
	if err == nil {
		defer deckRows.Close()
		for deckRows.Next() {
			var d models.Deck
			var cat string
			if err := deckRows.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &cat); err == nil {
				d.CreatedAt, _ = time.Parse(time.RFC3339, cat)
				p.Decks = append(p.Decks, d)
			}
		}
	}

	return p, nil
}

func (s *PlaylistService) Delete(userID, playlistID string) error {
	result, err := s.db.Exec("DELETE FROM playlists WHERE id = ? AND user_id = ?", playlistID, userID)
	if err != nil {
		return fmt.Errorf("delete playlist: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("playlist not found")
	}
	return nil
}

func (s *PlaylistService) verifyOwnership(userID, playlistID string) error {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM playlists WHERE id = ? AND user_id = ?", playlistID, userID).Scan(&count)
	if err != nil || count == 0 {
		return fmt.Errorf("playlist not found")
	}
	return nil
}

func (s *PlaylistService) verifyArticleOwnership(userID, articleID string) error {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM articles WHERE id = ? AND user_id = ?", articleID, userID).Scan(&count)
	if err != nil || count == 0 {
		return fmt.Errorf("article not found")
	}
	return nil
}

func (s *PlaylistService) verifyDeckOwnership(userID, deckID string) error {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM decks WHERE id = ? AND user_id = ?", deckID, userID).Scan(&count)
	if err != nil || count == 0 {
		return fmt.Errorf("deck not found")
	}
	return nil
}

func (s *PlaylistService) LinkArticle(userID, playlistID, articleID string) error {
	if err := s.verifyOwnership(userID, playlistID); err != nil {
		return err
	}
	if err := s.verifyArticleOwnership(userID, articleID); err != nil {
		return err
	}

	_, err := s.db.Exec("INSERT OR IGNORE INTO playlist_articles (playlist_id, article_id) VALUES (?, ?)", playlistID, articleID)
	if err != nil {
		return fmt.Errorf("link article: %w", err)
	}
	return nil
}

func (s *PlaylistService) UnlinkArticle(userID, playlistID, articleID string) error {
	if err := s.verifyOwnership(userID, playlistID); err != nil {
		return err
	}

	_, err := s.db.Exec("DELETE FROM playlist_articles WHERE playlist_id = ? AND article_id = ?", playlistID, articleID)
	if err != nil {
		return fmt.Errorf("unlink article: %w", err)
	}
	return nil
}

func (s *PlaylistService) LinkDeck(userID, playlistID, deckID string) error {
	if err := s.verifyOwnership(userID, playlistID); err != nil {
		return err
	}
	if err := s.verifyDeckOwnership(userID, deckID); err != nil {
		return err
	}

	_, err := s.db.Exec("INSERT OR IGNORE INTO playlist_decks (playlist_id, deck_id) VALUES (?, ?)", playlistID, deckID)
	if err != nil {
		return fmt.Errorf("link deck: %w", err)
	}
	return nil
}

func (s *PlaylistService) UnlinkDeck(userID, playlistID, deckID string) error {
	if err := s.verifyOwnership(userID, playlistID); err != nil {
		return err
	}

	_, err := s.db.Exec("DELETE FROM playlist_decks WHERE playlist_id = ? AND deck_id = ?", playlistID, deckID)
	if err != nil {
		return fmt.Errorf("unlink deck: %w", err)
	}
	return nil
}

func (s *PlaylistService) ListForArticle(userID, articleID string) ([]models.Playlist, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.user_id, p.title, p.description, p.url, p.type, p.external_id, p.created_at
		FROM playlists p
		JOIN playlist_articles pa ON pa.playlist_id = p.id
		WHERE pa.article_id = ? AND p.user_id = ?
	`, articleID, userID)
	if err != nil {
		return nil, fmt.Errorf("list playlists for article: %w", err)
	}
	defer rows.Close()

	var playlists []models.Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		playlists = append(playlists, *p)
	}
	return playlists, nil
}

func (s *PlaylistService) ListForDeck(userID, deckID string) ([]models.Playlist, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.user_id, p.title, p.description, p.url, p.type, p.external_id, p.created_at
		FROM playlists p
		JOIN playlist_decks pd ON pd.playlist_id = p.id
		WHERE pd.deck_id = ? AND p.user_id = ?
	`, deckID, userID)
	if err != nil {
		return nil, fmt.Errorf("list playlists for deck: %w", err)
	}
	defer rows.Close()

	var playlists []models.Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		playlists = append(playlists, *p)
	}
	return playlists, nil
}

func parsePlaylistURL(rawURL string) (string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}

	host := strings.ToLower(u.Host)

	// Spotify: open.spotify.com/playlist/{id}
	if host == "open.spotify.com" || host == "www.open.spotify.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "playlist" {
			return models.PlaylistTypeSpotify, parts[1], nil
		}
		return "", "", fmt.Errorf("invalid Spotify playlist URL")
	}

	// YouTube: youtube.com/playlist?list={id} or www.youtube.com/playlist?list={id}
	if host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" {
		if strings.HasPrefix(u.Path, "/playlist") {
			listID := u.Query().Get("list")
			if listID != "" {
				return models.PlaylistTypeYouTube, listID, nil
			}
		}
		return "", "", fmt.Errorf("invalid YouTube playlist URL")
	}

	// music.youtube.com
	if host == "music.youtube.com" {
		listID := u.Query().Get("list")
		if listID != "" {
			return models.PlaylistTypeYouTube, listID, nil
		}
		return "", "", fmt.Errorf("invalid YouTube Music playlist URL")
	}

	return "", "", fmt.Errorf("unsupported URL: only Spotify and YouTube playlist URLs are supported")
}

func scanPlaylist(row scannable) (*models.Playlist, error) {
	var p models.Playlist
	var createdAt string
	err := row.Scan(&p.ID, &p.UserID, &p.Title, &p.Description, &p.URL, &p.Type, &p.ExternalID, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("scan playlist: %w", err)
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &p, nil
}
