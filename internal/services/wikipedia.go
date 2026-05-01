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
