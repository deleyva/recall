package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type GeminiService struct {
	apiKey     string
	httpClient *http.Client
}

func NewGeminiService(apiKey string) *GeminiService {
	return &GeminiService{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *GeminiService) IsConfigured() bool {
	return s.apiKey != ""
}

type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (s *GeminiService) GenerateFlashcards(content string, existing []models.Card, count int) ([]FlashcardPair, error) {
	// Truncate content to avoid token limits
	if len(content) > 30000 {
		content = content[:30000]
	}

	// Build prompt
	var prompt strings.Builder
	prompt.WriteString("You are a flashcard generator. Create exactly ")
	prompt.WriteString(fmt.Sprintf("%d", count))
	prompt.WriteString(" flashcards from the following article content.\n\n")

	if len(existing) > 0 {
		prompt.WriteString("The following flashcards already exist for this article. Create NEW flashcards covering DIFFERENT content:\n")
		for _, c := range existing {
			prompt.WriteString(fmt.Sprintf("- Q: %s / A: %s\n", c.Front, c.Back))
		}
		prompt.WriteString("\n")
	}

	prompt.WriteString("Article content:\n")
	prompt.WriteString(content)
	prompt.WriteString("\n\nRespond ONLY with a JSON array of objects with \"front\" and \"back\" keys. No markdown, no explanation. Example: [{\"front\":\"What is X?\",\"back\":\"X is Y.\"}]")

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: prompt.String()}}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=%s", s.apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBytes, &gemResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	text := gemResp.Candidates[0].Content.Parts[0].Text
	return parseFlashcardJSON(text)
}

// parseFlashcardJSON extracts flashcard pairs from potentially markdown-wrapped JSON
func parseFlashcardJSON(text string) ([]FlashcardPair, error) {
	// Strip markdown code fences if present
	re := regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)\\s*```")
	if matches := re.FindStringSubmatch(text); len(matches) > 1 {
		text = matches[1]
	}
	text = strings.TrimSpace(text)

	var pairs []FlashcardPair
	if err := json.Unmarshal([]byte(text), &pairs); err != nil {
		return nil, fmt.Errorf("parse flashcards JSON: %w (raw: %.200s)", err, text)
	}

	// Filter out empty pairs
	var valid []FlashcardPair
	for _, p := range pairs {
		if p.Front != "" && p.Back != "" {
			valid = append(valid, p)
		}
	}
	return valid, nil
}
