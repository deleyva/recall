package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

const geminiAPIURL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"

type GeminiService struct {
	apiKey     string
	httpClient *http.Client
}

func NewGeminiService(apiKey string) *GeminiService {
	return &GeminiService{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *GeminiService) IsConfigured() bool {
	return s.apiKey != ""
}

type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
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

// callGemini sends the assembled contents to the Gemini API and returns the raw text response.
func (s *GeminiService) callGemini(contents []geminiContent) (string, error) {
	reqBody := geminiRequest{Contents: contents}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s?key=%s", geminiAPIURL, s.apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBytes, &gemResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}

	return gemResp.Candidates[0].Content.Parts[0].Text, nil
}

// truncateUTF8 truncates a string to at most maxBytes bytes at a rune boundary.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Find the last valid rune boundary at or before maxBytes
	truncated := s[:maxBytes]
	for len(truncated) > 0 {
		r := truncated[len(truncated)-1]
		if r < 0x80 || r >= 0xC0 {
			break
		}
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

// DefaultFlashcardPrompt is the system default prompt template for flashcard generation.
// Users can override this via their profile settings.
// The placeholder {count} is replaced with the number of cards to generate.
const DefaultFlashcardPrompt = `You are a flashcard generator. Create exactly {count} flashcards from the following article content.

FORMATTING RULES:
- The "back" field MUST use HTML formatting for readability.
- Use <strong> to highlight key terms and important concepts.
- When listing items without a specific order, use <ul><li>...</li></ul>.
- When listing items in a specific sequence or ranking, use <ol><li>...</li></ol>.
- Never use raw numbered text like "1. item". Always use proper HTML list tags.
- Keep the "front" field as a clear, concise question (plain text, no HTML).
- CRITICAL: Write both front and back in the SAME LANGUAGE as the article content. If the article is in Spanish, the flashcards must be in Spanish. If in English, in English. Match the article's language exactly.`

func (s *GeminiService) GenerateFlashcards(content string, existing []models.Card, count int, customPrompt string) ([]FlashcardPair, error) {
	content = truncateUTF8(content, 30000)

	// Use custom prompt if provided, otherwise default
	promptTemplate := DefaultFlashcardPrompt
	if strings.TrimSpace(customPrompt) != "" {
		promptTemplate = customPrompt
	}

	// Build prompt
	var prompt strings.Builder
	prompt.WriteString(strings.ReplaceAll(promptTemplate, "{count}", fmt.Sprintf("%d", count)))
	prompt.WriteString("\n\n")

	if len(existing) > 0 {
		prompt.WriteString("The following flashcards already exist for this article. CRITICAL: Do NOT create flashcards about topics already covered below, even from a different angle or with different wording. If a concept, fact, or theme appears in ANY existing card, skip it entirely and find a completely unrelated topic from the article:\n")
		for _, c := range existing {
			prompt.WriteString(fmt.Sprintf("- Q: %s / A: %s\n", c.Front, c.Back))
		}
		prompt.WriteString("\n")
	}

	prompt.WriteString("Article content:\n")
	prompt.WriteString(content)
	prompt.WriteString("\n\nRespond ONLY with a JSON array of objects with \"front\" and \"back\" keys. No markdown, no explanation. Example: [{\"front\":\"What is X?\",\"back\":\"<strong>X</strong> is a concept that includes:<ul><li>First aspect</li><li>Second aspect</li></ul>\"}]")

	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: prompt.String()}}},
	}

	text, err := s.callGemini(contents)
	if err != nil {
		return nil, err
	}
	return parseFlashcardJSON(text)
}

func (s *GeminiService) ChatWithArticle(articleContent string, history []models.ChatMessage, userQuestion string) (string, error) {
	articleContent = truncateUTF8(articleContent, 20000)

	// Build multi-turn conversation with proper roles
	var contents []geminiContent

	// System-like first turn: set context with article
	systemPrompt := fmt.Sprintf(`You are a helpful study assistant. The user is studying an article and will ask questions about it. Answer based on the article content. Always respond in the same language as the article.

Article content:
%s`, articleContent)

	contents = append(contents, geminiContent{
		Role:  "user",
		Parts: []geminiPart{{Text: systemPrompt}},
	})

	// Model acknowledgment to complete the user-model pair
	contents = append(contents, geminiContent{
		Role:  "model",
		Parts: []geminiPart{{Text: "I've read the article. Ask me anything about it."}},
	})

	// Add chat history (last 20 messages max) with correct roles
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	for _, msg := range history {
		role := "user"
		if msg.Role == models.RoleAssistant {
			role = "model" // Gemini API uses "model", not "assistant"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}

	// Add current user question
	contents = append(contents, geminiContent{
		Role:  "user",
		Parts: []geminiPart{{Text: userQuestion}},
	})

	text, err := s.callGemini(contents)
	if err != nil {
		return "", err
	}

	// Sanitize HTML output — allow only safe formatting tags
	text = sanitizeChatHTML(text)
	return text, nil
}

// sanitizeChatHTML strips dangerous HTML from LLM output, keeping only safe formatting tags.
func sanitizeChatHTML(s string) string {
	// Allow: strong, em, ul, ol, li, p, br, h1-h6, code, pre, blockquote
	// Strip everything else (scripts, onclick, etc.)
	allowedTags := map[string]bool{
		"strong": true, "em": true, "b": true, "i": true,
		"ul": true, "ol": true, "li": true, "p": true, "br": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"code": true, "pre": true, "blockquote": true,
	}

	// Simple tag stripper: find all HTML tags, keep allowed ones, strip the rest
	re := regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)\b[^>]*>`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		submatch := re.FindStringSubmatch(match)
		if len(submatch) < 3 {
			return ""
		}
		tag := strings.ToLower(submatch[2])
		if allowedTags[tag] {
			// Reconstruct clean tag without attributes
			if submatch[1] == "/" {
				return "</" + tag + ">"
			}
			if tag == "br" {
				return "<br>"
			}
			return "<" + tag + ">"
		}
		return ""
	})
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
		log.Printf("Failed to parse flashcards JSON: %v (raw: %.200s)", err, text)
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
