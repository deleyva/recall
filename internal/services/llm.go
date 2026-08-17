package services

import (
	"bytes"
	"database/sql"
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

// FallbackAPIURL and FallbackModel are the last resort, used only when neither
// the instance env vars nor the user's own setting say otherwise. They are NOT
// the normal way to change the model: providers retire models (Groq dropped the
// whole llama-3.3 family in August 2026, which broke every generation call with
// a 404), and recompiling the binary is far too heavy a response to a one-word
// change. Precedence, most specific first: users.llm_model → LLM_MODEL → this.
const FallbackAPIURL = "https://api.groq.com/openai/v1/chat/completions"
const FallbackModel = "openai/gpt-oss-120b"

type LLMService struct {
	apiKey       string
	apiURL       string
	defaultModel string
	db           *sql.DB
	httpClient   *http.Client
}

// NewLLMService wires the instance defaults. apiURL and defaultModel may be
// empty, in which case the compiled fallbacks apply. db is used to read each
// user's own model override and may be nil (then every call uses the default).
func NewLLMService(apiKey, apiURL, defaultModel string, db *sql.DB) *LLMService {
	if strings.TrimSpace(apiURL) == "" {
		apiURL = FallbackAPIURL
	}
	return &LLMService{
		apiKey:       apiKey,
		apiURL:       apiURL,
		defaultModel: strings.TrimSpace(defaultModel),
		db:           db,
		httpClient:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (s *LLMService) IsConfigured() bool {
	return s.apiKey != ""
}

// ResolveModel applies the precedence chain for a given user. An empty userID
// (background work with no owner) skips the per-user lookup.
func (s *LLMService) ResolveModel(userID string) string {
	if s.db != nil && strings.TrimSpace(userID) != "" {
		var userModel string
		if err := s.db.QueryRow("SELECT llm_model FROM users WHERE id = ?", userID).Scan(&userModel); err == nil {
			if m := strings.TrimSpace(userModel); m != "" {
				return m
			}
		}
	}
	if s.defaultModel != "" {
		return s.defaultModel
	}
	return FallbackModel
}

// OpenAI-compatible request/response types
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (s *LLMService) callLLM(messages []chatMessage, model string) (string, error) {
	if strings.TrimSpace(model) == "" {
		model = FallbackModel
	}
	reqBody := chatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.7,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", s.apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Name the model in the error: the most common failure here is a
		// provider retiring it, and the fix is to change the model setting —
		// which is only obvious if the message says which model was tried.
		return "", fmt.Errorf("LLM API error (status %d, model %q): %s", resp.StatusCode, model, string(respBytes))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from LLM")
	}

	return chatResp.Choices[0].Message.Content, nil
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
- CRITICAL: Write both front and back in the SAME LANGUAGE as the article content. If the article is in Spanish, the flashcards must be in Spanish. If in English, in English. Match the article's language exactly.
- LANGUAGE DETECTION: Before generating any flashcard, detect the language of the article. Then write ALL flashcards entirely in that detected language. NEVER default to English if the article is not in English. Every word of every flashcard — questions and answers — must be in the article's language.`

func (s *LLMService) GenerateFlashcards(content string, existing []models.Card, count int, customPrompt string, userID string) ([]FlashcardPair, error) {
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

	messages := []chatMessage{
		{Role: "system", Content: "You are a multilingual flashcard generator. CRITICAL: Detect the language of the article content provided and generate ALL output exclusively in that language. If the article is in Spanish, every flashcard must be in Spanish. If in French, in French. NEVER default to English unless the article itself is in English. Respond ONLY with a JSON array."},
		{Role: "user", Content: prompt.String()},
	}

	text, err := s.callLLM(messages, s.ResolveModel(userID))
	if err != nil {
		return nil, err
	}
	return parseFlashcardJSON(text)
}

func (s *LLMService) ChatWithArticle(articleContent string, history []models.ChatMessage, userQuestion string, userID string) (string, error) {
	articleContent = truncateUTF8(articleContent, 20000)

	var messages []chatMessage

	// System message with article context
	systemPrompt := fmt.Sprintf(`You are a helpful study assistant. The user is studying an article and will ask questions about it. Answer based on the article content. Always respond in the same language as the article.

Article content:
%s`, articleContent)

	messages = append(messages, chatMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	// Add chat history (last 20 messages max)
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	for _, msg := range history {
		role := "user"
		if msg.Role == models.RoleAssistant {
			role = "assistant"
		}
		messages = append(messages, chatMessage{
			Role:    role,
			Content: msg.Content,
		})
	}

	// Add current user question
	messages = append(messages, chatMessage{
		Role:    "user",
		Content: userQuestion,
	})

	text, err := s.callLLM(messages, s.ResolveModel(userID))
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
