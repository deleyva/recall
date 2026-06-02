package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type WikipediaService struct {
	httpClient *http.Client
}

func NewWikipediaService() *WikipediaService {
	return &WikipediaService{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// IsWikipediaURL checks if a URL is from Wikipedia
func IsWikipediaURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.HasSuffix(host, "wikipedia.org")
}

// GetArticleImages fetches images for a Wikipedia article URL
func (s *WikipediaService) GetArticleImages(articleURL string) ([]models.WikiImage, error) {
	parsed, err := url.Parse(articleURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	// Extract article title from URL path (e.g., /wiki/Bossa_nova)
	path := parsed.Path
	if !strings.HasPrefix(path, "/wiki/") {
		return nil, fmt.Errorf("not a Wikipedia article URL")
	}
	title := strings.TrimPrefix(path, "/wiki/")

	// Determine API base from hostname
	apiBase := fmt.Sprintf("https://%s/w/api.php", parsed.Hostname())

	// Step 1: Get list of images
	imageNames, err := s.fetchImageList(apiBase, title)
	if err != nil {
		return nil, err
	}

	if len(imageNames) == 0 {
		return nil, nil
	}

	// Filter out non-image files (SVGs, icons, etc.)
	var filtered []string
	for _, name := range imageNames {
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".svg") || strings.Contains(lower, "icon") ||
			strings.Contains(lower, "logo") || strings.Contains(lower, "flag_of") ||
			strings.Contains(lower, "commons-emblem") || strings.Contains(lower, "edit-") ||
			strings.Contains(lower, "question_book") || strings.Contains(lower, "ambox") {
			continue
		}
		filtered = append(filtered, name)
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	// Cap at 20 images
	if len(filtered) > 20 {
		filtered = filtered[:20]
	}

	// Step 2: Get image info (URLs + descriptions) in batches of 10
	var allImages []models.WikiImage
	for i := 0; i < len(filtered); i += 10 {
		end := i + 10
		if end > len(filtered) {
			end = len(filtered)
		}
		batch, err := s.fetchImageInfo(apiBase, filtered[i:end])
		if err != nil {
			continue
		}
		allImages = append(allImages, batch...)
	}

	return allImages, nil
}

type wikiImageListResponse struct {
	Query struct {
		Pages map[string]struct {
			Images []struct {
				Title string `json:"title"`
			} `json:"images"`
		} `json:"pages"`
	} `json:"query"`
}

func (s *WikipediaService) fetchImageList(apiBase, title string) ([]string, error) {
	u := fmt.Sprintf("%s?action=query&titles=%s&prop=images&format=json&imlimit=50",
		apiBase, url.QueryEscape(title))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Recall/1.0 (Personal reading app; contact: recall@leyvitando.synology.me)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read image list: %w", err)
	}

	var result wikiImageListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse image list: %w", err)
	}

	var names []string
	for _, page := range result.Query.Pages {
		for _, img := range page.Images {
			names = append(names, img.Title)
		}
	}
	return names, nil
}

type wikiImageInfoResponse struct {
	Query struct {
		Pages map[string]struct {
			Title     string `json:"title"`
			ImageInfo []struct {
				URL          string `json:"url"`
				ThumbURL     string `json:"thumburl"`
				ExtMetadata  map[string]struct {
					Value interface{} `json:"value"`
				} `json:"extmetadata"`
			} `json:"imageinfo"`
		} `json:"pages"`
	} `json:"query"`
}

func (s *WikipediaService) fetchImageInfo(apiBase string, titles []string) ([]models.WikiImage, error) {
	joinedTitles := strings.Join(titles, "|")
	u := fmt.Sprintf("%s?action=query&titles=%s&prop=imageinfo&iiprop=url|extmetadata&iiurlwidth=800&format=json",
		apiBase, url.QueryEscape(joinedTitles))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Recall/1.0 (Personal reading app; contact: recall@leyvitando.synology.me)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read image info: %w", err)
	}

	var result wikiImageInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse image info: %w", err)
	}

	var images []models.WikiImage
	for _, page := range result.Query.Pages {
		if len(page.ImageInfo) == 0 {
			continue
		}
		info := page.ImageInfo[0]

		description := ""
		if desc, ok := info.ExtMetadata["ImageDescription"]; ok {
			if s, ok := desc.Value.(string); ok {
				// Strip HTML tags from description
				description = stripHTMLTags(s)
			}
		}

		imgURL := info.URL
		thumbURL := info.ThumbURL
		if thumbURL == "" {
			thumbURL = imgURL
		}

		if imgURL != "" {
			images = append(images, models.WikiImage{
				URL:         imgURL,
				ThumbURL:    thumbURL,
				Description: description,
				Title:       page.Title,
			})
		}
	}
	return images, nil
}

// WikiSummary holds summary data for a Wikipedia article
type WikiSummary struct {
	Title    string
	Extract  string
	ThumbURL string
	Links    []string
}

// ParseWikiURL extracts the article title and API base from a Wikipedia URL
func ParseWikiURL(rawURL string) (apiBase, title, lang string, err error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("parse URL: %w", err)
	}
	host := parsed.Hostname()
	if !strings.HasSuffix(host, "wikipedia.org") {
		return "", "", "", fmt.Errorf("not a Wikipedia URL")
	}
	path := parsed.Path
	if !strings.HasPrefix(path, "/wiki/") {
		return "", "", "", fmt.Errorf("not a Wikipedia article URL")
	}
	title = strings.TrimPrefix(path, "/wiki/")
	apiBase = fmt.Sprintf("https://%s/w/api.php", host)
	// Extract language from subdomain (e.g., "es" from "es.wikipedia.org")
	parts := strings.Split(host, ".")
	if len(parts) >= 3 {
		lang = parts[0]
	} else {
		lang = "en"
	}
	return apiBase, title, lang, nil
}

type wikiSummaryResponse struct {
	Query struct {
		Pages map[string]struct {
			Title    string `json:"title"`
			Extract  string `json:"extract"`
			Thumbnail struct {
				Source string `json:"source"`
			} `json:"thumbnail"`
			Links []struct {
				Title string `json:"title"`
			} `json:"links"`
		} `json:"pages"`
	} `json:"query"`
}

// GetArticleSummaryAndLinks fetches title, extract, thumbnail, and top links for a Wikipedia URL
func (s *WikipediaService) GetArticleSummaryAndLinks(wikiURL string) (*WikiSummary, error) {
	apiBase, title, _, err := ParseWikiURL(wikiURL)
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("%s?action=query&titles=%s&prop=extracts|pageimages|links&exintro=true&exsentences=2&pithumbsize=300&plnamespace=0&pllimit=500&format=json",
		apiBase, url.QueryEscape(title))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Recall/1.0 (Personal reading app; contact: recall@leyvitando.synology.me)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch summary: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read summary: %w", err)
	}

	var result wikiSummaryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse summary: %w", err)
	}

	for _, page := range result.Query.Pages {
		summary := &WikiSummary{
			Title:    page.Title,
			Extract:  stripHTMLTags(page.Extract),
			ThumbURL: page.Thumbnail.Source,
		}
		for _, link := range page.Links {
			summary.Links = append(summary.Links, link.Title)
		}
		return summary, nil
	}
	return nil, fmt.Errorf("no pages found")
}

type wikiBatchResponse struct {
	Query struct {
		Pages map[string]struct {
			Title     string `json:"title"`
			Extract   string `json:"extract"`
			Thumbnail struct {
				Source string `json:"source"`
			} `json:"thumbnail"`
		} `json:"pages"`
	} `json:"query"`
}

// BatchGetSummaries fetches summaries and thumbnails for multiple titles (max 10 per call)
func (s *WikipediaService) BatchGetSummaries(apiBase string, titles []string) (map[string]WikiSummary, error) {
	if len(titles) > 10 {
		titles = titles[:10]
	}

	joinedTitles := strings.Join(titles, "|")
	u := fmt.Sprintf("%s?action=query&titles=%s&prop=extracts|pageimages&exintro=true&exsentences=2&pithumbsize=300&format=json",
		apiBase, url.QueryEscape(joinedTitles))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Recall/1.0 (Personal reading app; contact: recall@leyvitando.synology.me)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch batch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read batch: %w", err)
	}

	var result wikiBatchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse batch: %w", err)
	}

	summaries := make(map[string]WikiSummary)
	for _, page := range result.Query.Pages {
		summaries[page.Title] = WikiSummary{
			Title:    page.Title,
			Extract:  stripHTMLTags(page.Extract),
			ThumbURL: page.Thumbnail.Source,
		}
	}
	return summaries, nil
}

// ScoreRelevance computes word overlap between seed and link titles (exported for testing)
func ScoreRelevance(seedTitle, linkTitle string) float64 {
	seedWords := strings.Fields(strings.ToLower(seedTitle))
	linkWords := strings.Fields(strings.ToLower(linkTitle))

	if len(seedWords) == 0 || len(linkWords) == 0 {
		return 0
	}

	seedSet := make(map[string]bool)
	for _, w := range seedWords {
		seedSet[w] = true
	}

	overlap := 0
	for _, w := range linkWords {
		if seedSet[w] {
			overlap++
		}
	}

	return float64(overlap) / float64(len(linkWords))
}

// IsFilteredLink checks if a Wikipedia link should be filtered out
func IsFilteredLink(title string) bool {
	lower := strings.ToLower(title)
	prefixes := []string{"list of", "lista de", "index of", "outline of", "category:", "wikipedia:", "template:", "help:", "portal:", "file:", "special:", "anexo:"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	suffixes := []string{"(disambiguation)", "(desambiguación)", "(desambiguacion)"}
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	// Filter bare years (e.g. "1600", "2024") and decade/century patterns
	if isNumericTitle(title) {
		return true
	}
	return false
}

// isNumericTitle returns true for titles that are just numbers (years),
// or common date-like patterns like "Años 1600", "Década de 1600", "Siglo XVI"
func isNumericTitle(title string) bool {
	trimmed := strings.TrimSpace(title)
	// Pure number (year)
	allDigits := true
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits && len(trimmed) >= 3 && len(trimmed) <= 4 {
		return true
	}
	// Patterns like "Años 1600", "Década de 1600", "1600s"
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "años ") || strings.HasPrefix(lower, "década") {
		return true
	}
	if len(lower) >= 4 && lower[len(lower)-1] == 's' {
		rest := lower[:len(lower)-1]
		allD := true
		for _, r := range rest {
			if r < '0' || r > '9' {
				allD = false
				break
			}
		}
		if allD {
			return true
		}
	}
	return false
}

// SearchArticles searches Wikipedia for articles matching a query
func (s *WikipediaService) SearchArticles(query, lang string) ([]WikiSummary, error) {
	if lang == "" {
		lang = "en"
	}
	apiBase := fmt.Sprintf("https://%s.wikipedia.org/w/api.php", lang)
	u := fmt.Sprintf("%s?action=query&list=search&srsearch=%s&srlimit=10&format=json",
		apiBase, url.QueryEscape(query))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Recall/1.0 (Personal reading app; contact: recall@leyvitando.synology.me)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var summaries []WikiSummary
	for _, s := range result.Query.Search {
		wikiURL := fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", lang, url.PathEscape(s.Title))
		summaries = append(summaries, WikiSummary{
			Title:   s.Title,
			Extract: stripHTMLTags(s.Snippet),
			Links:   []string{wikiURL},
		})
	}
	return summaries, nil
}

func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}
