package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type ReadeckService struct {
	db       *sql.DB
	articles *ArticleService
}

type readeckBookmark struct {
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	TextContent string   `json:"text_content"`
	Labels      []string `json:"labels"`
}

type readeckUserConfig struct {
	UserID   string
	URL      string
	APIToken string
}

func NewReadeckService(db *sql.DB, articles *ArticleService) *ReadeckService {
	return &ReadeckService{db: db, articles: articles}
}

// SyncAllUsers runs the Readeck sync for every user that has configured it.
func (s *ReadeckService) SyncAllUsers() {
	configs, err := s.getConfiguredUsers()
	if err != nil {
		log.Printf("[readeck-sync] Failed to get configured users: %v", err)
		return
	}

	if len(configs) == 0 {
		return
	}

	for _, cfg := range configs {
		s.syncForUser(cfg)
	}
}

func (s *ReadeckService) getConfiguredUsers() ([]readeckUserConfig, error) {
	rows, err := s.db.Query(`
		SELECT id, readeck_url, readeck_api_token FROM users
		WHERE readeck_url != '' AND readeck_api_token != ''
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []readeckUserConfig
	for rows.Next() {
		var c readeckUserConfig
		if err := rows.Scan(&c.UserID, &c.URL, &c.APIToken); err != nil {
			continue
		}
		c.URL = strings.TrimRight(c.URL, "/")
		configs = append(configs, c)
	}
	return configs, nil
}

func (s *ReadeckService) syncForUser(cfg readeckUserConfig) {
	log.Printf("[readeck-sync] Syncing for user %s", cfg.UserID)

	bookmarks, err := fetchTagged(cfg, "recall")
	if err != nil {
		log.Printf("[readeck-sync] Failed to fetch bookmarks for user %s: %v", cfg.UserID, err)
		return
	}

	if len(bookmarks) == 0 {
		return
	}

	imported := 0
	for _, bm := range bookmarks {
		_, err := s.articles.CreateDirect(cfg.UserID, bm.URL, bm.Title, bm.TextContent)
		if err != nil {
			if strings.Contains(err.Error(), "already added") {
				log.Printf("[readeck-sync] Skipping duplicate: %s", bm.Title)
			} else {
				log.Printf("[readeck-sync] Failed to import '%s': %v", bm.Title, err)
				continue
			}
		} else {
			imported++
			log.Printf("[readeck-sync] Imported: %s", bm.Title)
		}

		// Relabel in Readeck (even if duplicate, mark as processed)
		if err := relabel(cfg, bm.ID, bm.Labels); err != nil {
			log.Printf("[readeck-sync] Failed to relabel '%s': %v", bm.Title, err)
		}
	}

	log.Printf("[readeck-sync] User %s: %d imported out of %d tagged", cfg.UserID, imported, len(bookmarks))
}

func fetchTagged(cfg readeckUserConfig, label string) ([]readeckBookmark, error) {
	url := fmt.Sprintf("%s/api/bookmarks?labels=%s&is_archived=false&limit=20", cfg.URL, label)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var bookmarks []readeckBookmark
	if err := json.NewDecoder(resp.Body).Decode(&bookmarks); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return bookmarks, nil
}

func relabel(cfg readeckUserConfig, bookmarkID string, currentLabels []string) error {
	newLabels := []string{}
	for _, l := range currentLabels {
		if l != "recall" {
			newLabels = append(newLabels, l)
		}
	}
	newLabels = append(newLabels, "recall-imported")

	body, _ := json.Marshal(map[string]interface{}{
		"labels": newLabels,
	})

	url := fmt.Sprintf("%s/api/bookmarks/%s", cfg.URL, bookmarkID)
	req, err := http.NewRequest("PATCH", url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("relabel request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("relabel status %d", resp.StatusCode)
	}

	return nil
}
