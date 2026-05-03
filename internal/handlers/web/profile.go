package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type ProfileHandler struct {
	tokens *services.TokenService
	tmpl   *templates.Registry
	db     *sql.DB
}

func NewProfileHandler(tokens *services.TokenService, tmpl *templates.Registry, db *sql.DB) *ProfileHandler {
	return &ProfileHandler{tokens: tokens, tmpl: tmpl, db: db}
}

func (h *ProfileHandler) ProfilePage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	tokens, _ := h.tokens.List(userID)

	var dailyCardLimit int
	var readeckURL, readeckToken string
	h.db.QueryRow("SELECT daily_card_limit, readeck_url, readeck_api_token FROM users WHERE id = ?", userID).Scan(&dailyCardLimit, &readeckURL, &readeckToken)
	if dailyCardLimit == 0 {
		dailyCardLimit = 5
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "profile.html", map[string]interface{}{
		"Tokens":         tokens,
		"Email":          c.Get(middleware.EmailKey),
		"NewToken":       c.QueryParam("new_token"),
		"Error":          c.QueryParam("error"),
		"Success":        c.QueryParam("success"),
		"DailyCardLimit": dailyCardLimit,
		"ReadeckURL":     readeckURL,
		"ReadeckToken":   readeckToken,
	})
}

func (h *ProfileHandler) UpdateSettings(c echo.Context) error {
	userID := middleware.GetUserID(c)

	limit, err := strconv.Atoi(c.FormValue("daily_card_limit"))
	if err != nil || limit < 1 || limit > 20 {
		return c.Redirect(http.StatusSeeOther, "/profile?error=Daily+card+limit+must+be+between+1+and+20")
	}

	readeckURL := strings.TrimSpace(c.FormValue("readeck_url"))
	readeckToken := strings.TrimSpace(c.FormValue("readeck_api_token"))

	_, err = h.db.Exec(
		"UPDATE users SET daily_card_limit = ?, readeck_url = ?, readeck_api_token = ? WHERE id = ?",
		limit, readeckURL, readeckToken, userID,
	)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/profile?error="+fmt.Sprintf("Failed+to+save:+%v", err))
	}

	return c.Redirect(http.StatusSeeOther, "/profile?success=Settings+saved")
}

func (h *ProfileHandler) CreateToken(c echo.Context) error {
	userID := middleware.GetUserID(c)
	name := c.FormValue("name")
	if name == "" {
		name = "API Token"
	}

	rawToken, _, err := h.tokens.Create(userID, name)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/profile?error=Could+not+create+token")
	}

	return c.Redirect(http.StatusSeeOther, "/profile?new_token="+rawToken+"&success=Token+created")
}

func (h *ProfileHandler) DeleteToken(c echo.Context) error {
	userID := middleware.GetUserID(c)
	tokenID := c.Param("id")

	if err := h.tokens.Delete(userID, tokenID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/profile?error=Could+not+delete+token")
	}

	return c.Redirect(http.StatusSeeOther, "/profile?success=Token+deleted")
}
