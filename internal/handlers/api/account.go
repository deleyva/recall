package api

import (
	"net/http"
	"strings"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/models"
	"github.com/deleyva/recall/internal/services"
	"github.com/labstack/echo/v4"
)

// userSettings is the editable half of a user record. Readeck credentials are
// write-only: the token goes in, it never comes back out.
type userSettings struct {
	// Three distinct limits. DailyCardLimit bounds how many cards the nightly
	// job CREATES; the two study limits bound how many the queue SERVES. They
	// answer different questions and are stored, labelled and validated apart.
	DailyCardLimit      int    `json:"daily_card_limit"`
	DailyNewLimit       int    `json:"daily_new_limit"`
	DailyReviewLimit    int    `json:"daily_review_limit"`
	PodcastEnabled      bool   `json:"podcast_enabled"`
	FlashcardGenEnabled bool   `json:"flashcard_gen_enabled"`
	FlashcardPrompt     string `json:"flashcard_prompt"`
	ReadeckURL          string `json:"readeck_url"`
	ReadeckConfigured   bool   `json:"readeck_configured"`
	// LLMModel is this user's override, empty when they follow the instance
	// default. LLMModelEffective is what a call would actually use right now —
	// the two differ precisely when the override is empty.
	LLMModel          string `json:"llm_model"`
	LLMModelEffective string `json:"llm_model_effective"`
}

func (h *Handler) GetMe(c echo.Context) error {
	userID := middleware.GetUserID(c)

	var (
		email, createdAt, readeckURL, readeckToken, prompt, llmModel string
		limit, podcast, gen, isAdmin, newLimit, reviewLimit          int
	)
	err := h.db.QueryRow(`
		SELECT email, created_at, daily_card_limit, readeck_url, readeck_api_token,
			podcast_enabled, flashcard_prompt, flashcard_gen_enabled, is_admin, llm_model,
			daily_new_limit, daily_review_limit
		FROM users WHERE id = ?`, userID).
		Scan(&email, &createdAt, &limit, &readeckURL, &readeckToken, &podcast, &prompt, &gen, &isAdmin, &llmModel,
			&newLimit, &reviewLimit)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "user not found"})
	}
	if prompt == "" {
		prompt = services.DefaultFlashcardPrompt
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"id":         userID,
		"email":      email,
		"is_admin":   isAdmin == 1,
		"created_at": createdAt,
		"settings": userSettings{
			DailyCardLimit:      limit,
			DailyNewLimit:       newLimit,
			DailyReviewLimit:    reviewLimit,
			PodcastEnabled:      podcast == 1,
			FlashcardGenEnabled: gen == 1,
			FlashcardPrompt:     prompt,
			ReadeckURL:          readeckURL,
			ReadeckConfigured:   readeckToken != "",
			LLMModel:            llmModel,
			LLMModelEffective:   h.llm.ResolveModel(userID),
		},
	})
}

func (h *Handler) UpdateMySettings(c echo.Context) error {
	userID := middleware.GetUserID(c)

	// Pointers so "absent" and "set to zero/false" stay distinguishable.
	var req struct {
		DailyCardLimit      *int    `json:"daily_card_limit"`
		DailyNewLimit       *int    `json:"daily_new_limit"`
		DailyReviewLimit    *int    `json:"daily_review_limit"`
		PodcastEnabled      *bool   `json:"podcast_enabled"`
		FlashcardGenEnabled *bool   `json:"flashcard_gen_enabled"`
		FlashcardPrompt     *string `json:"flashcard_prompt"`
		ReadeckURL          *string `json:"readeck_url"`
		ReadeckAPIToken     *string `json:"readeck_api_token"`
		LLMModel            *string `json:"llm_model"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	sets := []string{}
	args := []interface{}{}

	if req.DailyCardLimit != nil {
		if *req.DailyCardLimit < 1 || *req.DailyCardLimit > 20 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "daily_card_limit must be 1-20"})
		}
		sets = append(sets, "daily_card_limit = ?")
		args = append(args, *req.DailyCardLimit)
	}
	// Each limit is set only when it is named, so writing one never disturbs
	// another. Zero is allowed for the study limits: it means none today.
	if req.DailyNewLimit != nil {
		if *req.DailyNewLimit < 0 || *req.DailyNewLimit > 500 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "daily_new_limit must be 0-500"})
		}
		sets = append(sets, "daily_new_limit = ?")
		args = append(args, *req.DailyNewLimit)
	}
	if req.DailyReviewLimit != nil {
		if *req.DailyReviewLimit < 0 || *req.DailyReviewLimit > 9999 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "daily_review_limit must be 0-9999"})
		}
		sets = append(sets, "daily_review_limit = ?")
		args = append(args, *req.DailyReviewLimit)
	}
	if req.PodcastEnabled != nil {
		sets = append(sets, "podcast_enabled = ?")
		args = append(args, boolToInt(*req.PodcastEnabled))
	}
	if req.FlashcardGenEnabled != nil {
		sets = append(sets, "flashcard_gen_enabled = ?")
		args = append(args, boolToInt(*req.FlashcardGenEnabled))
	}
	if req.FlashcardPrompt != nil {
		prompt := strings.TrimSpace(*req.FlashcardPrompt)
		if prompt == services.DefaultFlashcardPrompt {
			prompt = "" // empty means "use the system default"
		}
		sets = append(sets, "flashcard_prompt = ?")
		args = append(args, prompt)
	}
	if req.ReadeckURL != nil {
		sets = append(sets, "readeck_url = ?")
		args = append(args, strings.TrimSpace(*req.ReadeckURL))
	}
	if req.ReadeckAPIToken != nil {
		sets = append(sets, "readeck_api_token = ?")
		args = append(args, strings.TrimSpace(*req.ReadeckAPIToken))
	}
	if req.LLMModel != nil {
		// Empty is meaningful: it hands the choice back to the instance default.
		sets = append(sets, "llm_model = ?")
		args = append(args, strings.TrimSpace(*req.LLMModel))
	}

	if len(sets) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no settings provided"})
	}

	args = append(args, userID)
	if _, err := h.db.Exec("UPDATE users SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return h.GetMe(c)
}

func (h *Handler) ListTokens(c echo.Context) error {
	userID := middleware.GetUserID(c)
	tokens, err := h.tokens.List(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if tokens == nil {
		tokens = []models.APIToken{}
	}
	return c.JSON(http.StatusOK, tokens)
}

// CreateToken returns the raw token exactly once — it is hashed at rest.
func (h *Handler) CreateToken(c echo.Context) error {
	userID := middleware.GetUserID(c)
	var req struct {
		Name string `json:"name"`
	}
	c.Bind(&req)
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "API Token"
	}

	raw, token, err := h.tokens.Create(userID, req.Name)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"token": raw,
		"meta":  token,
	})
}

func (h *Handler) DeleteToken(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if err := h.tokens.Delete(userID, c.Param("id")); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "token not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "deleted"})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
