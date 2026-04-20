package web

import (
	"net/http"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type ProfileHandler struct {
	tokens *services.TokenService
	tmpl   *templates.Registry
}

func NewProfileHandler(tokens *services.TokenService, tmpl *templates.Registry) *ProfileHandler {
	return &ProfileHandler{tokens: tokens, tmpl: tmpl}
}

func (h *ProfileHandler) ProfilePage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	tokens, _ := h.tokens.List(userID)

	return h.tmpl.ExecuteTemplate(c.Response(), "profile.html", map[string]interface{}{
		"Tokens":   tokens,
		"Email":    c.Get(middleware.EmailKey),
		"NewToken": c.QueryParam("new_token"),
		"Error":    c.QueryParam("error"),
		"Success":  c.QueryParam("success"),
	})
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
